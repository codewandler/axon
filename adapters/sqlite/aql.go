package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
)

// existsRewrite holds information for rewriting EXISTS patterns as CTE+JOIN.
// This optimization transforms slow correlated subqueries into fast CTE-based joins.
type existsRewrite struct {
	cteSQL     string // WITH clause (e.g., "WITH __descendants_0(...) ")
	cteArgs    []any  // Arguments for CTE
	joinCTE    string // CTE name to JOIN from (empty if NOT EXISTS or no rewrite)
	joinColumn string // Column to join on: "id" for nodes, "from_id" for edges
	whereSQL   string // Modified WHERE clause (EXISTS removed, other conditions kept)
	whereArgs  []any  // Arguments for WHERE
}

// jsonExtractRegex matches json_extract(data, '$.field') to extract the field name.
var jsonExtractRegex = regexp.MustCompile(`json_extract\([^,]+,\s*'\$\.(\w+)'`)

// Query executes an AQL query and returns results.
//
// Phase 1 (current): Supports flat queries on nodes/edges tables with:
//   - SELECT *, columns, COUNT(*)
//   - WHERE with all expression types
//   - GROUP BY, HAVING
//   - ORDER BY, LIMIT, OFFSET
//
// Phase 2 (future): Simple pattern queries without recursion
// Phase 3 (future): Recursive patterns with CTEs
//
// TODO: Query cache - cache compiled SQL by hash of query AST.
// This would be beneficial when same query runs repeatedly (e.g., CLI polling).
// Consider using sync.Map or LRU cache with ~1000 entry limit.
func (s *Storage) Query(ctx context.Context, query interface{}) (*graph.QueryResult, error) {
	q, ok := query.(*aql.Query)
	if !ok {
		return nil, fmt.Errorf("query must be *aql.Query, got %T", query)
	}

	// Validate
	if errs := aql.Validate(q); len(errs) > 0 {
		return nil, &QueryError{
			Query: q,
			Phase: "validate",
			Err:   fmt.Errorf("validation errors: %v", errs),
		}
	}

	// Compile to SQL
	sql, args, resultType, err := s.compileToSQL(q)
	if err != nil {
		return nil, &QueryError{
			Query: q,
			Phase: "compile",
			Err:   err,
		}
	}

	// Execute
	result, err := s.executeQuery(ctx, q, sql, args, resultType)
	if err != nil {
		return nil, &QueryError{
			Query: q,
			Phase: "execute",
			Err:   err,
		}
	}

	return result, nil
}

// Explain returns the execution plan for an AQL query without executing it.
// Useful for debugging and performance analysis.
func (s *Storage) Explain(ctx context.Context, query interface{}) (*graph.QueryPlan, error) {
	q, ok := query.(*aql.Query)
	if !ok {
		return nil, fmt.Errorf("query must be *aql.Query, got %T", query)
	}

	// Validate
	if errs := aql.Validate(q); len(errs) > 0 {
		return nil, &QueryError{
			Query: q,
			Phase: "validate",
			Err:   fmt.Errorf("validation errors: %v", errs),
		}
	}

	// Compile to SQL
	sqlQuery, args, _, err := s.compileToSQL(q)
	if err != nil {
		return nil, &QueryError{
			Query: q,
			Phase: "compile",
			Err:   err,
		}
	}

	// Get SQLite query plan
	explainSQL := "EXPLAIN QUERY PLAN " + sqlQuery
	rows, err := s.db.QueryContext(ctx, explainSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("explain query failed: %w", err)
	}
	defer rows.Close()

	var planLines []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			return nil, err
		}
		planLines = append(planLines, detail)
	}

	return &graph.QueryPlan{
		SQL:        sqlQuery,
		Args:       args,
		SQLitePlan: strings.Join(planLines, "\n"),
	}, nil
}

// compileToSQL converts AQL AST to SQLite query.
// Returns (sql, args, resultType, error).
func (s *Storage) compileToSQL(q *aql.Query) (string, []any, graph.ResultType, error) {
	switch src := q.Select.From.(type) {
	case *aql.TableSource:
		return s.compileFlatQuery(q)
	case *aql.JoinedTableSource:
		return s.compileJoinedTableQuery(q, src)
	case *aql.PatternSource:
		return s.compilePatternQuery(q, src)
	default:
		return "", nil, 0, fmt.Errorf("unknown source type: %T", src)
	}
}

// extractExistsCTEs detects optimizable EXISTS patterns and returns rewrite info.
// Returns nil if no optimization is possible.
func (s *Storage) extractExistsCTEs(where aql.Expression, table string) *existsRewrite {
	if where == nil {
		return nil
	}

	// Find optimizable EXISTS expressions
	existsExprs := s.findOptimizableExists(where, table)
	if len(existsExprs) == 0 {
		return nil
	}

	// For now, handle single EXISTS (most common case)
	// Multiple AND'd EXISTS would need CTE intersection - defer for later
	if len(existsExprs) > 1 {
		return nil
	}

	e := existsExprs[0]

	// NOT EXISTS uses different approach (NOT IN instead of JOIN)
	if e.Not {
		return s.buildNotExistsRewrite(e, where, table)
	}

	return s.buildExistsRewrite(e, where, table)
}

// findOptimizableExists finds all EXISTS expressions with variable-length paths that correlate with outer table.
func (s *Storage) findOptimizableExists(expr aql.Expression, table string) []*aql.ExistsExpr {
	var result []*aql.ExistsExpr

	var walk func(e aql.Expression)
	walk = func(e aql.Expression) {
		switch ex := e.(type) {
		case *aql.ExistsExpr:
			if s.isOptimizableExists(ex, table) {
				result = append(result, ex)
			}
		case *aql.BinaryExpr:
			walk(ex.Left)
			walk(ex.Right)
		case *aql.UnaryExpr:
			walk(ex.Operand)
		case *aql.ParenExpr:
			walk(ex.Inner)
		}
	}

	walk(expr)
	return result
}

// isOptimizableExists checks if an EXISTS expression can be optimized to a CTE.
func (s *Storage) isOptimizableExists(e *aql.ExistsExpr, table string) bool {
	if len(e.Pattern.Elements) < 3 {
		return false
	}

	// Must have variable-length edge
	hasVarLen := false
	for _, elem := range e.Pattern.Elements {
		if ep, ok := elem.(*aql.EdgePattern); ok && ep.IsVariableLength() {
			hasVarLen = true
			break
		}
	}

	if !hasVarLen {
		return false
	}

	// Target node must be present
	targetNode, ok := e.Pattern.Elements[len(e.Pattern.Elements)-1].(*aql.NodePattern)
	if !ok {
		return false
	}

	// For nodes table: target variable must match table name (correlate on id)
	// For edges table: any target variable works (correlate on from_id)
	if table == "nodes" {
		return targetNode.Variable == table
	}
	if table == "edges" {
		// Any target variable is OK for edges - we correlate on from_id
		return true
	}

	return false
}

// buildExistsRewrite creates rewrite info for EXISTS pattern (uses CTE+JOIN).
func (s *Storage) buildExistsRewrite(e *aql.ExistsExpr, fullWhere aql.Expression, table string) *existsRewrite {
	cteName := "__descendants_0"

	// Build CTE
	cteBody, cteArgs := s.buildDescendantsCTE(e, table, cteName)
	if cteBody == "" {
		return nil
	}
	cteSQL := "WITH " + cteBody + " "

	// Build WHERE with EXISTS removed, keeping other conditions
	whereSQL, whereArgs := s.compileWhereExcluding(fullWhere, e, table)

	// Determine correlation column based on table
	// - nodes: correlate on id (the node itself is a descendant)
	// - edges: correlate on from_id (the edge's source node is a descendant)
	joinColumn := "id"
	if table == "edges" {
		joinColumn = "from_id"
	}

	return &existsRewrite{
		cteSQL:     cteSQL,
		cteArgs:    cteArgs,
		joinCTE:    cteName,
		joinColumn: joinColumn,
		whereSQL:   whereSQL,
		whereArgs:  whereArgs,
	}
}

// buildNotExistsRewrite creates rewrite info for NOT EXISTS (uses CTE + NOT IN).
func (s *Storage) buildNotExistsRewrite(e *aql.ExistsExpr, fullWhere aql.Expression, table string) *existsRewrite {
	cteName := "__descendants_0"

	// Build CTE
	cteBody, cteArgs := s.buildDescendantsCTE(e, table, cteName)
	if cteBody == "" {
		return nil
	}
	cteSQL := "WITH " + cteBody + " "

	// Build WHERE: replace NOT EXISTS with NOT IN, keep other conditions
	whereSQL, whereArgs := s.compileWhereReplacingNotExists(fullWhere, e, table, cteName)

	// Determine correlation column based on table
	joinColumn := "id"
	if table == "edges" {
		joinColumn = "from_id"
	}

	return &existsRewrite{
		cteSQL:     cteSQL,
		cteArgs:    cteArgs,
		joinCTE:    "", // Empty = don't rewrite FROM, use NOT IN in WHERE
		joinColumn: joinColumn,
		whereSQL:   whereSQL,
		whereArgs:  whereArgs,
	}
}

// compileWhereExcluding compiles WHERE clause with the specified EXISTS expression removed.
func (s *Storage) compileWhereExcluding(expr aql.Expression, skip *aql.ExistsExpr, table string) (string, []any) {
	switch e := expr.(type) {
	case *aql.ExistsExpr:
		if e == skip {
			return "", nil // Skip this EXISTS
		}
		// Compile normally
		sql, args, _ := s.compileExists(e, table)
		return sql, args

	case *aql.BinaryExpr:
		leftSQL, leftArgs := s.compileWhereExcluding(e.Left, skip, table)
		rightSQL, rightArgs := s.compileWhereExcluding(e.Right, skip, table)

		// Handle case where one side was removed
		if leftSQL == "" {
			return rightSQL, rightArgs
		}
		if rightSQL == "" {
			return leftSQL, leftArgs
		}

		var args []any
		args = append(args, leftArgs...)
		args = append(args, rightArgs...)
		return fmt.Sprintf("%s %s %s", leftSQL, e.Op, rightSQL), args

	case *aql.ParenExpr:
		innerSQL, innerArgs := s.compileWhereExcluding(e.Inner, skip, table)
		if innerSQL == "" {
			return "", nil
		}
		return fmt.Sprintf("(%s)", innerSQL), innerArgs

	case *aql.UnaryExpr:
		// For NOT expr, compile the operand
		operandSQL, operandArgs := s.compileWhereExcluding(e.Operand, skip, table)
		if operandSQL == "" {
			return "", nil
		}
		return fmt.Sprintf("%s %s", e.Op, operandSQL), operandArgs

	default:
		// Compile normally
		sql, args, _ := s.compileWhere(e, table)
		return sql, args
	}
}

// compileWhereReplacingNotExists compiles WHERE, replacing NOT EXISTS with NOT IN.
func (s *Storage) compileWhereReplacingNotExists(expr aql.Expression, target *aql.ExistsExpr, table, cteName string) (string, []any) {
	switch e := expr.(type) {
	case *aql.ExistsExpr:
		if e == target {
			// Replace NOT EXISTS with NOT IN
			return fmt.Sprintf("%s.id NOT IN (SELECT node_id FROM %s)", table, cteName), nil
		}
		sql, args, _ := s.compileExists(e, table)
		return sql, args

	case *aql.BinaryExpr:
		leftSQL, leftArgs := s.compileWhereReplacingNotExists(e.Left, target, table, cteName)
		rightSQL, rightArgs := s.compileWhereReplacingNotExists(e.Right, target, table, cteName)
		var args []any
		args = append(args, leftArgs...)
		args = append(args, rightArgs...)
		return fmt.Sprintf("%s %s %s", leftSQL, e.Op, rightSQL), args

	case *aql.ParenExpr:
		innerSQL, innerArgs := s.compileWhereReplacingNotExists(e.Inner, target, table, cteName)
		return fmt.Sprintf("(%s)", innerSQL), innerArgs

	case *aql.UnaryExpr:
		operandSQL, operandArgs := s.compileWhereReplacingNotExists(e.Operand, target, table, cteName)
		return fmt.Sprintf("%s %s", e.Op, operandSQL), operandArgs

	default:
		sql, args, _ := s.compileWhere(e, table)
		return sql, args
	}
}

// buildDescendantsCTE builds a CTE that computes all descendants of a source node.
func (s *Storage) buildDescendantsCTE(e *aql.ExistsExpr, table, cteName string) (string, []any) {
	// Find the variable-length edge
	var varLenEdge *aql.EdgePattern
	var varLenIdx int
	for i, elem := range e.Pattern.Elements {
		if ep, ok := elem.(*aql.EdgePattern); ok && ep.IsVariableLength() {
			varLenEdge = ep
			varLenIdx = i
			break
		}
	}

	if varLenEdge == nil || varLenIdx == 0 || varLenIdx >= len(e.Pattern.Elements)-1 {
		return "", nil
	}

	sourceNode := e.Pattern.Elements[varLenIdx-1].(*aql.NodePattern)

	var sql strings.Builder
	var args []any

	sql.WriteString(cteName)
	sql.WriteString("(node_id) AS (WITH RECURSIVE path(node_id, depth) AS (")

	// Base case: start from source node
	sql.WriteString("SELECT id, 0 FROM nodes")

	// Add source node constraints
	if sourceNode.Type != "" || sourceNode.Where != nil {
		sql.WriteString(" WHERE ")
		if sourceNode.Type != "" {
			sql.WriteString("type = ?")
			args = append(args, sourceNode.Type)
			if sourceNode.Where != nil {
				sql.WriteString(" AND ")
			}
		}
		if sourceNode.Where != nil {
			whereSQL, whereArgs, err := s.compileWhere(sourceNode.Where, "nodes")
			if err != nil {
				return "", nil
			}
			sql.WriteString(whereSQL)
			args = append(args, whereArgs...)
		}
	}

	// Recursive case
	sql.WriteString(" UNION ALL SELECT ")
	switch varLenEdge.Direction {
	case aql.Outgoing:
		sql.WriteString("e.to_id")
	case aql.Incoming:
		sql.WriteString("e.from_id")
	default:
		return "", nil
	}

	// Use INDEXED BY hint for efficient traversal when edge type is specified
	// idx_edges_from_type is on (from_id, type) which is optimal for outgoing edges
	// For incoming edges, we rely on idx_edges_to_id_type (to_id, type)
	hasEdgeType := varLenEdge.Type != "" || len(varLenEdge.Types) > 0
	if hasEdgeType && varLenEdge.Direction == aql.Outgoing {
		sql.WriteString(", path.depth + 1 FROM path JOIN edges e INDEXED BY idx_edges_from_type ON ")
	} else if hasEdgeType && varLenEdge.Direction == aql.Incoming {
		sql.WriteString(", path.depth + 1 FROM path JOIN edges e INDEXED BY idx_edges_to_id_type ON ")
	} else {
		sql.WriteString(", path.depth + 1 FROM path JOIN edges e ON ")
	}

	switch varLenEdge.Direction {
	case aql.Outgoing:
		sql.WriteString("e.from_id = path.node_id")
	case aql.Incoming:
		sql.WriteString("e.to_id = path.node_id")
	}

	// Add edge type constraint
	if varLenEdge.Type != "" {
		sql.WriteString(" AND e.type = ?")
		args = append(args, varLenEdge.Type)
	} else if len(varLenEdge.Types) > 0 {
		sql.WriteString(" AND e.type IN (")
		for i, t := range varLenEdge.Types {
			if i > 0 {
				sql.WriteString(", ")
			}
			sql.WriteString("?")
			args = append(args, t)
		}
		sql.WriteString(")")
	}

	// Add depth constraints for recursive step
	if varLenEdge.MinHops != nil {
		sql.WriteString(fmt.Sprintf(" WHERE path.depth + 1 >= %d", *varLenEdge.MinHops))
		if varLenEdge.MaxHops != nil {
			sql.WriteString(fmt.Sprintf(" AND path.depth + 1 <= %d", *varLenEdge.MaxHops))
		}
	} else if varLenEdge.MaxHops != nil {
		sql.WriteString(fmt.Sprintf(" WHERE path.depth + 1 <= %d", *varLenEdge.MaxHops))
	}

	// Close recursive CTE and select valid depths
	sql.WriteString(") SELECT node_id FROM path WHERE ")
	if varLenEdge.MinHops != nil {
		sql.WriteString(fmt.Sprintf("depth >= %d", *varLenEdge.MinHops))
	} else {
		sql.WriteString("depth >= 0")
	}
	if varLenEdge.MaxHops != nil {
		sql.WriteString(fmt.Sprintf(" AND depth <= %d", *varLenEdge.MaxHops))
	}
	sql.WriteString(")")

	return sql.String(), args
}

// compileFlatQuery compiles SELECT from nodes/edges table.
func (s *Storage) compileFlatQuery(q *aql.Query) (string, []any, graph.ResultType, error) {
	src := q.Select.From.(*aql.TableSource)

	// Validate table
	if src.Table != "nodes" && src.Table != "edges" {
		return "", nil, 0, fmt.Errorf("invalid table: %s (must be 'nodes' or 'edges')", src.Table)
	}

	// Determine result type
	hasCount := false
	for _, col := range q.Select.Columns {
		if _, ok := col.Expr.(*aql.CountCall); ok {
			hasCount = true
			break
		}
	}

	var resultType graph.ResultType
	if hasCount {
		// Both scalar COUNT and GROUP BY COUNT return ResultTypeCounts
		resultType = graph.ResultTypeCounts
	} else if src.Table == "nodes" {
		resultType = graph.ResultTypeNodes
	} else {
		resultType = graph.ResultTypeEdges
	}

	var sqlBuilder strings.Builder
	var args []any

	// Optimize: Extract EXISTS patterns with variable-length paths into CTEs
	// to avoid correlated subqueries
	rewrite := s.extractExistsCTEs(q.Select.Where, src.Table)

	// CTE clause (if rewriting)
	if rewrite != nil && rewrite.cteSQL != "" {
		sqlBuilder.WriteString(rewrite.cteSQL)
		args = append(args, rewrite.cteArgs...)
	}

	// SELECT clause - always use table prefix when rewriting with JOIN
	if rewrite != nil && rewrite.joinCTE != "" {
		selectSQL, err := s.compileSelectWithPrefix(q.Select, src.Table)
		if err != nil {
			return "", nil, 0, err
		}
		sqlBuilder.WriteString(selectSQL)
	} else {
		selectSQL, err := s.compileSelect(q.Select, src.Table)
		if err != nil {
			return "", nil, 0, err
		}
		sqlBuilder.WriteString(selectSQL)
	}

	// FROM clause - JOIN with CTE when rewriting EXISTS
	if rewrite != nil && rewrite.joinCTE != "" {
		// FROM __descendants_0 JOIN table ON __descendants_0.node_id = table.joinColumn
		// For nodes: JOIN on id (the node itself is a descendant)
		// For edges: JOIN on from_id (the edge's source node is a descendant)
		sqlBuilder.WriteString(" FROM ")
		sqlBuilder.WriteString(rewrite.joinCTE)
		sqlBuilder.WriteString(" JOIN ")
		sqlBuilder.WriteString(src.Table)
		sqlBuilder.WriteString(" ON ")
		sqlBuilder.WriteString(rewrite.joinCTE)
		sqlBuilder.WriteString(".node_id = ")
		sqlBuilder.WriteString(src.Table)
		sqlBuilder.WriteString(".")
		sqlBuilder.WriteString(rewrite.joinColumn)
	} else {
		sqlBuilder.WriteString(" FROM ")
		sqlBuilder.WriteString(src.Table)
	}

	// WHERE clause
	// WHERE clause
	if rewrite != nil {
		// Use optimized WHERE (EXISTS removed or replaced with NOT IN)
		// When rewrite is active, we MUST use rewrite.whereSQL even if empty
		// because the EXISTS has been handled by the CTE+JOIN
		if rewrite.whereSQL != "" {
			sqlBuilder.WriteString(" WHERE ")
			sqlBuilder.WriteString(rewrite.whereSQL)
			args = append(args, rewrite.whereArgs...)
		}
		// If rewrite.whereSQL is empty, no WHERE clause needed
	} else if q.Select.Where != nil {
		// No rewrite - use original WHERE compilation
		whereSQL, whereArgs, err := s.compileWhere(q.Select.Where, src.Table)
		if err != nil {
			return "", nil, 0, err
		}
		sqlBuilder.WriteString(" WHERE ")
		sqlBuilder.WriteString(whereSQL)
		args = append(args, whereArgs...)
	}

	// GROUP BY clause
	if len(q.Select.GroupBy) > 0 {
		groupBySQL, err := s.compileGroupBy(q.Select.GroupBy, src.Table)
		if err != nil {
			return "", nil, 0, err
		}
		sqlBuilder.WriteString(" GROUP BY ")
		sqlBuilder.WriteString(groupBySQL)
	}

	// HAVING clause
	if q.Select.Having != nil {
		if len(q.Select.GroupBy) == 0 {
			return "", nil, 0, fmt.Errorf("HAVING requires GROUP BY")
		}
		havingSQL, havingArgs, err := s.compileWhere(q.Select.Having, src.Table)
		if err != nil {
			return "", nil, 0, err
		}
		sqlBuilder.WriteString(" HAVING ")
		sqlBuilder.WriteString(havingSQL)
		args = append(args, havingArgs...)
	}

	// ORDER BY clause
	if len(q.Select.OrderBy) > 0 {
		orderBySQL, err := s.compileOrderBy(q.Select.OrderBy, src.Table)
		if err != nil {
			return "", nil, 0, err
		}
		sqlBuilder.WriteString(" ORDER BY ")
		sqlBuilder.WriteString(orderBySQL)
	}

	// LIMIT/OFFSET
	if q.Select.Limit != nil {
		sqlBuilder.WriteString(" LIMIT ?")
		args = append(args, *q.Select.Limit)

		if q.Select.Offset != nil {
			sqlBuilder.WriteString(" OFFSET ?")
			args = append(args, *q.Select.Offset)
		}
	}

	return sqlBuilder.String(), args, resultType, nil
}

// compileJoinedTableQuery compiles SELECT from table with table-valued function.
// Example: SELECT value, COUNT(*) FROM nodes, json_each(labels) GROUP BY value
//
// Supported table functions:
//   - json_each(column) - unpacks JSON array into rows with 'key' and 'value' columns
//
// Also supports EXISTS patterns for scoping, e.g.:
//
//	SELECT value, COUNT(*) FROM nodes, json_each(labels)
//	WHERE EXISTS (root WHERE id='x')-[:contains*0..]->(nodes)
//	GROUP BY value
//
// These are optimized using CTE+JOIN rewrite for performance.
func (s *Storage) compileJoinedTableQuery(q *aql.Query, src *aql.JoinedTableSource) (string, []any, graph.ResultType, error) {
	// Validate table
	if src.Table != "nodes" && src.Table != "edges" {
		return "", nil, 0, fmt.Errorf("invalid table: %s (must be 'nodes' or 'edges')", src.Table)
	}

	// Validate table function
	if src.TableFunc == nil {
		return "", nil, 0, fmt.Errorf("joined table source requires a table function")
	}

	// Currently only json_each is supported
	if src.TableFunc.Name != "json_each" {
		return "", nil, 0, fmt.Errorf("unsupported table function: %s (only json_each is supported)", src.TableFunc.Name)
	}

	// Determine result type - joined queries always return counts (for aggregations)
	hasCount := false
	for _, col := range q.Select.Columns {
		if _, ok := col.Expr.(*aql.CountCall); ok {
			hasCount = true
			break
		}
	}

	var resultType graph.ResultType
	if hasCount {
		resultType = graph.ResultTypeCounts
	} else if len(q.Select.GroupBy) > 0 {
		resultType = graph.ResultTypeCounts
	} else {
		// No aggregation - return as nodes
		resultType = graph.ResultTypeNodes
	}

	var sqlBuilder strings.Builder
	var args []any

	// Optimize: Extract EXISTS patterns with variable-length paths into CTEs
	rewrite := s.extractExistsCTEs(q.Select.Where, src.Table)

	// CTE clause (if rewriting)
	if rewrite != nil && rewrite.cteSQL != "" {
		sqlBuilder.WriteString(rewrite.cteSQL)
		args = append(args, rewrite.cteArgs...)
	}

	// SELECT clause
	selectSQL, err := s.compileSelectForJoined(q.Select, src)
	if err != nil {
		return "", nil, 0, err
	}
	sqlBuilder.WriteString(selectSQL)

	// FROM clause
	if rewrite != nil && rewrite.joinCTE != "" {
		// FROM __descendants_0 JOIN table ON __descendants_0.node_id = table.joinColumn, json_each(table.column)
		// For nodes: JOIN on id (the node itself is a descendant)
		// For edges: JOIN on from_id (the edge's source node is a descendant)
		sqlBuilder.WriteString(" FROM ")
		sqlBuilder.WriteString(rewrite.joinCTE)
		sqlBuilder.WriteString(" JOIN ")
		sqlBuilder.WriteString(src.Table)
		sqlBuilder.WriteString(" ON ")
		sqlBuilder.WriteString(rewrite.joinCTE)
		sqlBuilder.WriteString(".node_id = ")
		sqlBuilder.WriteString(src.Table)
		sqlBuilder.WriteString(".")
		sqlBuilder.WriteString(rewrite.joinColumn)
		sqlBuilder.WriteString(", ")
	} else {
		// FROM table, json_each(table.column)
		sqlBuilder.WriteString(" FROM ")
		sqlBuilder.WriteString(src.Table)
		sqlBuilder.WriteString(", ")
	}

	// Build json_each call
	argSQL := s.compileTableFuncArg(src.TableFunc.Arg, src.Table)
	sqlBuilder.WriteString(src.TableFunc.Name)
	sqlBuilder.WriteString("(")
	sqlBuilder.WriteString(argSQL)
	sqlBuilder.WriteString(")")

	// WHERE clause
	if rewrite != nil {
		// Use optimized WHERE (EXISTS removed)
		if rewrite.whereSQL != "" {
			whereSQL, whereArgs := s.adaptWhereForJoined(rewrite.whereSQL, rewrite.whereArgs, src)
			sqlBuilder.WriteString(" WHERE ")
			sqlBuilder.WriteString(whereSQL)
			args = append(args, whereArgs...)
		}
	} else if q.Select.Where != nil {
		whereSQL, whereArgs, err := s.compileWhereForJoined(q.Select.Where, src)
		if err != nil {
			return "", nil, 0, err
		}
		sqlBuilder.WriteString(" WHERE ")
		sqlBuilder.WriteString(whereSQL)
		args = append(args, whereArgs...)
	}

	// GROUP BY clause
	if len(q.Select.GroupBy) > 0 {
		groupBySQL, err := s.compileGroupByForJoined(q.Select.GroupBy, src)
		if err != nil {
			return "", nil, 0, err
		}
		sqlBuilder.WriteString(" GROUP BY ")
		sqlBuilder.WriteString(groupBySQL)
	}

	// HAVING clause
	if q.Select.Having != nil {
		if len(q.Select.GroupBy) == 0 {
			return "", nil, 0, fmt.Errorf("HAVING requires GROUP BY")
		}
		havingSQL, havingArgs, err := s.compileWhereForJoined(q.Select.Having, src)
		if err != nil {
			return "", nil, 0, err
		}
		sqlBuilder.WriteString(" HAVING ")
		sqlBuilder.WriteString(havingSQL)
		args = append(args, havingArgs...)
	}

	// ORDER BY clause
	if len(q.Select.OrderBy) > 0 {
		orderBySQL, err := s.compileOrderByForJoined(q.Select.OrderBy, src)
		if err != nil {
			return "", nil, 0, err
		}
		sqlBuilder.WriteString(" ORDER BY ")
		sqlBuilder.WriteString(orderBySQL)
	}

	// LIMIT/OFFSET
	if q.Select.Limit != nil {
		sqlBuilder.WriteString(" LIMIT ?")
		args = append(args, *q.Select.Limit)

		if q.Select.Offset != nil {
			sqlBuilder.WriteString(" OFFSET ?")
			args = append(args, *q.Select.Offset)
		}
	}

	return sqlBuilder.String(), args, resultType, nil
}

// adaptWhereForJoined adapts a WHERE clause compiled for flat queries to work with joined queries.
// This handles the case where EXISTS was removed and we need to ensure remaining conditions
// reference columns correctly for the json_each context.
func (s *Storage) adaptWhereForJoined(whereSQL string, whereArgs []any, src *aql.JoinedTableSource) (string, []any) {
	// The WHERE clause from compileWhereExcluding uses table-qualified names (e.g., "nodes.type")
	// which work fine in the joined context. We just need to handle json_each columns (value, key)
	// which are already unqualified in the original WHERE compilation.
	// For now, pass through as-is since the flat query compilation should produce compatible SQL.
	return whereSQL, whereArgs
}

// compileTableFuncArg compiles the argument to a table function.
func (s *Storage) compileTableFuncArg(sel *aql.Selector, table string) string {
	if len(sel.Parts) == 1 {
		// Simple column: labels -> nodes.labels
		return table + "." + sel.Parts[0]
	}
	// JSON path: data.tags -> json_extract(nodes.data, '$.tags')
	if sel.Parts[0] == "data" {
		jsonPath := "$." + strings.Join(sel.Parts[1:], ".")
		return fmt.Sprintf("json_extract(%s.data, '%s')", table, jsonPath)
	}
	// Fallback: join with dots
	return table + "." + strings.Join(sel.Parts, ".")
}

// compileSelectForJoined compiles SELECT clause for joined table queries.
func (s *Storage) compileSelectForJoined(stmt *aql.SelectStmt, src *aql.JoinedTableSource) (string, error) {
	var parts []string

	for _, col := range stmt.Columns {
		switch expr := col.Expr.(type) {
		case *aql.Star:
			return "", fmt.Errorf("SELECT * not supported with table functions; select specific columns")
		case *aql.CountCall:
			if col.Alias != "" {
				parts = append(parts, fmt.Sprintf("COUNT(*) AS %s", col.Alias))
			} else {
				parts = append(parts, "COUNT(*)")
			}
		case *aql.Selector:
			sql := s.compileSelectorForJoined(expr, src)
			if col.Alias != "" {
				parts = append(parts, fmt.Sprintf("%s AS %s", sql, col.Alias))
			} else {
				parts = append(parts, sql)
			}
		default:
			return "", fmt.Errorf("unsupported column expression: %T", expr)
		}
	}

	result := "SELECT"
	if stmt.Distinct {
		result += " DISTINCT"
	}
	return result + " " + strings.Join(parts, ", "), nil
}

// compileSelectorForJoined compiles a selector in the context of a joined table query.
// It handles references to json_each output columns (key, value).
func (s *Storage) compileSelectorForJoined(sel *aql.Selector, src *aql.JoinedTableSource) string {
	if len(sel.Parts) == 1 {
		name := sel.Parts[0]
		// Check if it's a json_each output column
		if name == "value" || name == "key" {
			return name
		}
		// Check if it's an alias for the table function
		if src.TableFunc.Alias != "" && name == src.TableFunc.Alias {
			return "value" // Default to value for alias
		}
		// Regular table column
		return src.Table + "." + name
	}

	// Multi-part selector
	first := sel.Parts[0]

	// Check if first part matches table function alias
	if src.TableFunc.Alias != "" && first == src.TableFunc.Alias {
		// alias.value, alias.key
		if len(sel.Parts) == 2 {
			return sel.Parts[1]
		}
	}

	// Check if it's table.column
	if first == src.Table {
		rest := sel.Parts[1:]
		if len(rest) == 1 {
			return src.Table + "." + rest[0]
		}
		// JSON path in table: nodes.data.ext
		if rest[0] == "data" {
			jsonPath := "$." + strings.Join(rest[1:], ".")
			return fmt.Sprintf("json_extract(%s.data, '%s')", src.Table, jsonPath)
		}
	}

	// data.field -> json_extract
	if first == "data" {
		jsonPath := "$." + strings.Join(sel.Parts[1:], ".")
		return fmt.Sprintf("json_extract(%s.data, '%s')", src.Table, jsonPath)
	}

	return strings.Join(sel.Parts, ".")
}

// compileWhereForJoined compiles WHERE for joined table queries.
func (s *Storage) compileWhereForJoined(expr aql.Expression, src *aql.JoinedTableSource) (string, []any, error) {
	switch e := expr.(type) {
	case *aql.ComparisonExpr:
		left := s.compileSelectorForJoined(e.Left, src)
		val, err := s.compileValue(e.Right)
		if err != nil {
			return "", nil, err
		}
		op := s.compileComparisonOp(e.Op)
		return fmt.Sprintf("%s %s ?", left, op), []any{val}, nil

	case *aql.BinaryExpr:
		leftSQL, leftArgs, err := s.compileWhereForJoined(e.Left, src)
		if err != nil {
			return "", nil, err
		}
		rightSQL, rightArgs, err := s.compileWhereForJoined(e.Right, src)
		if err != nil {
			return "", nil, err
		}
		var args []any
		args = append(args, leftArgs...)
		args = append(args, rightArgs...)
		return fmt.Sprintf("%s %s %s", leftSQL, e.Op, rightSQL), args, nil

	case *aql.UnaryExpr:
		operandSQL, operandArgs, err := s.compileWhereForJoined(e.Operand, src)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%s %s", e.Op, operandSQL), operandArgs, nil

	case *aql.ParenExpr:
		innerSQL, innerArgs, err := s.compileWhereForJoined(e.Inner, src)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("(%s)", innerSQL), innerArgs, nil

	case *aql.IsNullExpr:
		sql := s.compileSelectorForJoined(e.Selector, src)
		if e.Not {
			return sql + " IS NOT NULL", nil, nil
		}
		return sql + " IS NULL", nil, nil

	case *aql.InExpr:
		left := s.compileSelectorForJoined(e.Left, src)
		op := "IN"
		if e.Not {
			op = "NOT IN"
		}
		// Subquery: IN (SELECT ...)
		if e.Subquery != nil {
			innerSQL, innerArgs, _, err := s.compileToSQL(e.Subquery)
			if err != nil {
				return "", nil, fmt.Errorf("subquery in %s: %w", op, err)
			}
			return fmt.Sprintf("%s %s (%s)", left, op, innerSQL), innerArgs, nil
		}
		var args []any
		placeholders := make([]string, len(e.Values))
		for i, v := range e.Values {
			val, err := s.compileValue(v)
			if err != nil {
				return "", nil, err
			}
			placeholders[i] = "?"
			args = append(args, val)
		}
		return fmt.Sprintf("%s %s (%s)", left, op, strings.Join(placeholders, ", ")), args, nil

	default:
		return "", nil, fmt.Errorf("unsupported expression type in joined query: %T", expr)
	}
}

// compileGroupByForJoined compiles GROUP BY for joined table queries.
func (s *Storage) compileGroupByForJoined(selectors []aql.Selector, src *aql.JoinedTableSource) (string, error) {
	var parts []string
	for _, sel := range selectors {
		parts = append(parts, s.compileSelectorForJoined(&sel, src))
	}
	return strings.Join(parts, ", "), nil
}

// compileOrderByForJoined compiles ORDER BY for joined table queries.
func (s *Storage) compileOrderByForJoined(orders []aql.OrderSpec, src *aql.JoinedTableSource) (string, error) {
	var parts []string
	for _, o := range orders {
		var sql string
		switch expr := o.Expr.(type) {
		case *aql.CountCall:
			sql = "COUNT(*)"
		case *aql.Selector:
			sql = s.compileSelectorForJoined(expr, src)
		default:
			return "", fmt.Errorf("unsupported ORDER BY expression: %T", expr)
		}
		if o.Descending {
			sql += " DESC"
		}
		parts = append(parts, sql)
	}
	return strings.Join(parts, ", "), nil
}

// compilePatternQuery compiles SELECT from pattern(s) using JOINs.
//
// Phase 2: Supports simple patterns without recursion:
//   - Single-hop patterns: (a)-[:type]->(b)
//   - Node type constraints: (a:fs:dir)
//   - Edge variables: (a)-[e:contains]->(b)
//   - Multi-type edges: [:contains|has]
//   - All directions: ->, <-, -
//   - Multiple patterns with shared variables (implicit JOINs)
//
// Phase 3 (future): Variable-length paths, EXISTS subqueries
func (s *Storage) compilePatternQuery(q *aql.Query, src *aql.PatternSource) (string, []any, graph.ResultType, error) {
	// Check if any patterns have variable-length edges
	hasVariableLength := false
	for _, pattern := range src.Patterns {
		for _, elem := range pattern.Elements {
			if edge, ok := elem.(*aql.EdgePattern); ok {
				if edge.IsVariableLength() {
					hasVariableLength = true
					break
				}
			}
		}
		if hasVariableLength {
			break
		}
	}

	// Use recursive CTE compilation for variable-length patterns
	if hasVariableLength {
		return s.compileVariableLengthPattern(q, src)
	}

	// Build the FROM clause with JOINs
	var sqlBuilder strings.Builder
	var args []any
	var whereClauses []string

	// Track which variables we've seen to handle multiple patterns
	nodeVars := make(map[string]bool)
	edgeVars := make(map[string]bool)

	// Track table aliases
	nodeAliases := make(map[string]string) // variable -> table alias
	edgeAliases := make(map[string]string) // variable -> table alias
	edgeCount := 0

	// Process each pattern
	for patternIdx, pattern := range src.Patterns {
		if len(pattern.Elements) == 0 {
			continue
		}

		// Pattern must be: node [edge node]*
		// Validate pattern structure
		for i, elem := range pattern.Elements {
			if i%2 == 0 {
				// Even positions should be nodes
				if _, ok := elem.(*aql.NodePattern); !ok {
					return "", nil, 0, fmt.Errorf("pattern must alternate between nodes and edges")
				}
			} else {
				// Odd positions should be edges
				if _, ok := elem.(*aql.EdgePattern); !ok {
					return "", nil, 0, fmt.Errorf("pattern must alternate between nodes and edges")
				}
			}
		}

		// Process node-edge-node triples
		for i := 0; i < len(pattern.Elements); i += 2 {
			leftNode := pattern.Elements[i].(*aql.NodePattern)

			// First node in the pattern - only create alias if not seen before
			if !nodeVars[leftNode.Variable] {
				leftAlias := fmt.Sprintf("n%d", len(nodeAliases))
				nodeAliases[leftNode.Variable] = leftAlias
				nodeVars[leftNode.Variable] = true

				if patternIdx == 0 && i == 0 {
					// First table in FROM
					sqlBuilder.WriteString("FROM nodes AS ")
					sqlBuilder.WriteString(leftAlias)
				}

				// Add type constraint if specified
				if leftNode.Type != "" {
					if strings.Contains(leftNode.Type, "*") {
						whereClauses = append(whereClauses, fmt.Sprintf("%s.type GLOB ?", leftAlias))
					} else {
						whereClauses = append(whereClauses, fmt.Sprintf("%s.type = ?", leftAlias))
					}
					args = append(args, leftNode.Type)
				}
			}

			// If there's an edge following
			if i+1 < len(pattern.Elements) {
				edge := pattern.Elements[i+1].(*aql.EdgePattern)
				rightNode := pattern.Elements[i+2].(*aql.NodePattern)

				edgeAlias := fmt.Sprintf("e%d", edgeCount)
				edgeCount++

				if edge.Variable != "" {
					edgeVars[edge.Variable] = true
					edgeAliases[edge.Variable] = edgeAlias
				}

				// Determine if rightNode is new or reused
				rightAlias := nodeAliases[rightNode.Variable]
				isNewNode := rightAlias == ""
				if isNewNode {
					rightAlias = fmt.Sprintf("n%d", len(nodeAliases))
					nodeAliases[rightNode.Variable] = rightAlias
					nodeVars[rightNode.Variable] = true
				}

				leftAlias := nodeAliases[leftNode.Variable]

				// Add JOIN for edge
				sqlBuilder.WriteString("\nJOIN edges AS ")
				sqlBuilder.WriteString(edgeAlias)
				sqlBuilder.WriteString(" ON ")

				// Join condition based on direction
				switch edge.Direction {
				case aql.Outgoing:
					fmt.Fprintf(&sqlBuilder, "%s.from_id = %s.id", edgeAlias, leftAlias)
				case aql.Incoming:
					fmt.Fprintf(&sqlBuilder, "%s.to_id = %s.id", edgeAlias, leftAlias)
				case aql.Undirected:
					// For undirected: (from_id = left.id) OR (to_id = left.id)
					fmt.Fprintf(&sqlBuilder, "(%s.from_id = %s.id OR %s.to_id = %s.id)",
						edgeAlias, leftAlias, edgeAlias, leftAlias)
				}

				// Add JOIN for right node
				if isNewNode {
					sqlBuilder.WriteString("\nJOIN nodes AS ")
					sqlBuilder.WriteString(rightAlias)
					sqlBuilder.WriteString(" ON ")
				} else {
					sqlBuilder.WriteString(" AND ")
				}

				switch edge.Direction {
				case aql.Outgoing:
					fmt.Fprintf(&sqlBuilder, "%s.id = %s.to_id", rightAlias, edgeAlias)
				case aql.Incoming:
					fmt.Fprintf(&sqlBuilder, "%s.id = %s.from_id", rightAlias, edgeAlias)
				case aql.Undirected:
					// For undirected: match the opposite end of whichever direction matched
					// (from=left AND right=to) OR (to=left AND right=from)
					fmt.Fprintf(&sqlBuilder, "((%s.from_id = %s.id AND %s.id = %s.to_id) OR (%s.to_id = %s.id AND %s.id = %s.from_id))",
						edgeAlias, leftAlias, rightAlias, edgeAlias,
						edgeAlias, leftAlias, rightAlias, edgeAlias)
				}

				// Add edge type constraint
				if len(edge.Types) > 1 {
					// Multi-type: IN (type1, type2, ...)
					placeholders := make([]string, len(edge.Types))
					for i := range edge.Types {
						placeholders[i] = "?"
						args = append(args, edge.Types[i])
					}
					whereClauses = append(whereClauses, fmt.Sprintf("%s.type IN (%s)", edgeAlias, strings.Join(placeholders, ", ")))
				} else if edge.Type != "" {
					if strings.Contains(edge.Type, "*") {
						whereClauses = append(whereClauses, fmt.Sprintf("%s.type GLOB ?", edgeAlias))
					} else {
						whereClauses = append(whereClauses, fmt.Sprintf("%s.type = ?", edgeAlias))
					}
					args = append(args, edge.Type)
				}

				// Add right node type constraint if specified
				if rightNode.Type != "" {
					if strings.Contains(rightNode.Type, "*") {
						whereClauses = append(whereClauses, fmt.Sprintf("%s.type GLOB ?", rightAlias))
					} else {
						whereClauses = append(whereClauses, fmt.Sprintf("%s.type = ?", rightAlias))
					}
					args = append(args, rightNode.Type)
				}
			}
		}
	}

	// Determine result type based on what's being selected
	resultType := graph.ResultTypeNodes // Default

	// Check if we have COUNT(*) with GROUP BY
	hasCount := false
	for _, col := range q.Select.Columns {
		if _, ok := col.Expr.(*aql.CountCall); ok {
			hasCount = true
			break
		}
	}
	if hasCount && len(q.Select.GroupBy) > 0 {
		resultType = graph.ResultTypeCounts
	} else {
		// Check if we're selecting edge variables
		for _, col := range q.Select.Columns {
			if sel, ok := col.Expr.(*aql.Selector); ok {
				if len(sel.Parts) == 1 {
					varName := sel.Parts[0]
					if _, ok := edgeAliases[varName]; ok {
						resultType = graph.ResultTypeEdges
						break
					}
				}
			}
		}
	}

	// Build SELECT clause - for now, assume selecting node variables
	// TODO: Handle COUNT(*), etc.
	selectSQL, err := s.compilePatternSelect(q.Select, nodeAliases, edgeAliases)
	if err != nil {
		return "", nil, 0, err
	}

	// Combine everything
	sql := selectSQL + "\n" + sqlBuilder.String()

	// Add WHERE clause from pattern constraints
	if len(whereClauses) > 0 {
		sql += "\nWHERE " + strings.Join(whereClauses, " AND ")
	}

	// Add user WHERE clause if present
	if q.Select.Where != nil {
		userWhere, userArgs, err := s.compilePatternWhere(q.Select.Where, nodeAliases, edgeAliases)
		if err != nil {
			return "", nil, 0, err
		}
		if len(whereClauses) > 0 {
			sql += " AND (" + userWhere + ")"
		} else {
			sql += "\nWHERE " + userWhere
		}
		args = append(args, userArgs...)
	}

	// Add GROUP BY if present
	if len(q.Select.GroupBy) > 0 {
		groupBySQL, err := s.compilePatternGroupBy(q.Select.GroupBy, nodeAliases, edgeAliases)
		if err != nil {
			return "", nil, 0, err
		}
		sql += "\nGROUP BY " + groupBySQL
	}

	// Add HAVING if present
	if q.Select.Having != nil {
		if len(q.Select.GroupBy) == 0 {
			return "", nil, 0, fmt.Errorf("HAVING requires GROUP BY")
		}
		havingSQL, havingArgs, err := s.compilePatternWhere(q.Select.Having, nodeAliases, edgeAliases)
		if err != nil {
			return "", nil, 0, err
		}
		sql += "\nHAVING " + havingSQL
		args = append(args, havingArgs...)
	}

	// Add ORDER BY if present
	if len(q.Select.OrderBy) > 0 {
		orderBySQL, err := s.compilePatternOrderBy(q.Select.OrderBy, nodeAliases, edgeAliases)
		if err != nil {
			return "", nil, 0, err
		}
		sql += "\nORDER BY " + orderBySQL
	}

	// Add LIMIT/OFFSET
	if q.Select.Limit != nil {
		sql += "\nLIMIT ?"
		args = append(args, *q.Select.Limit)
	}
	if q.Select.Offset != nil {
		sql += " OFFSET ?"
		args = append(args, *q.Select.Offset)
	}

	return sql, args, resultType, nil
}

// compileVariableLengthPattern compiles patterns with variable-length edges using recursive CTEs.
// Phase 3: Supports patterns like (a)-[:type*1..3]->(b)
func (s *Storage) compileVariableLengthPattern(q *aql.Query, src *aql.PatternSource) (string, []any, graph.ResultType, error) {
	// For now, only support single pattern with single variable-length edge
	if len(src.Patterns) > 1 {
		return "", nil, 0, fmt.Errorf("multiple patterns with variable-length edges not yet supported")
	}

	pattern := src.Patterns[0]

	// Find the variable-length edge
	var varLenEdgeIdx int = -1
	for i, elem := range pattern.Elements {
		if edge, ok := elem.(*aql.EdgePattern); ok {
			if edge.IsVariableLength() {
				if varLenEdgeIdx >= 0 {
					return "", nil, 0, fmt.Errorf("only one variable-length edge per pattern is supported")
				}
				varLenEdgeIdx = i
			}
		}
	}

	if varLenEdgeIdx < 0 {
		return "", nil, 0, fmt.Errorf("no variable-length edge found")
	}

	// Pattern should be: (start_node) -[edge*min..max]-> (end_node)
	// varLenEdgeIdx should be 1 (after first node)
	if varLenEdgeIdx != 1 || len(pattern.Elements) != 3 {
		return "", nil, 0, fmt.Errorf("variable-length patterns must be: (start)-[:type*min..max]->(end)")
	}

	startNode := pattern.Elements[0].(*aql.NodePattern)
	edge := pattern.Elements[1].(*aql.EdgePattern)
	endNode := pattern.Elements[2].(*aql.NodePattern)

	// Get min/max hops
	minHops := 1
	if edge.MinHops != nil {
		minHops = *edge.MinHops
	}
	maxHops := -1 // -1 means unbounded
	if edge.MaxHops != nil {
		maxHops = *edge.MaxHops
	}

	// For unbounded paths, cap at a reasonable depth limit.
	// Filesystem trees rarely exceed 20 levels; this avoids the CTE
	// materialization penalty while covering all practical depths.
	if maxHops < 0 {
		maxHops = 20
	}

	// Use unrolled JOIN chains - dramatically faster than CTEs because
	// SQLite can push LIMIT through JOINs but must fully materialize
	// CTEs before applying LIMIT.
	return s.compileUnrolledVariableLength(q, startNode, edge, endNode, minHops, maxHops)
}

// compileUnrolledVariableLength generates a UNION ALL of JOIN chains for each
// hop depth. This is dramatically faster than a recursive CTE because SQLite
// can push LIMIT through JOINs, whereas CTEs are fully materialized first.
//
// For (start:T1)-[:type*1..3]->(end:T2) LIMIT 5, this generates:
//
//	SELECT n_end.* FROM edges e0 ... JOIN nodes n_end ... WHERE ... -- 1 hop
//	UNION ALL
//	SELECT n_end.* FROM edges e0 JOIN edges e1 ... JOIN nodes n_end ... -- 2 hops
//	UNION ALL
//	SELECT n_end.* FROM edges e0 JOIN edges e1 JOIN edges e2 ... -- 3 hops
//	LIMIT 5
func (s *Storage) compileUnrolledVariableLength(q *aql.Query, startNode *aql.NodePattern, edge *aql.EdgePattern, endNode *aql.NodePattern, minHops, maxHops int) (string, []any, graph.ResultType, error) {
	var args []any
	var sql strings.Builder
	hasEdgeType := edge.Type != "" || len(edge.Types) > 0

	// Determine which node to select
	selectVar := ""
	if len(q.Select.Columns) > 0 {
		if sel, ok := q.Select.Columns[0].Expr.(*aql.Selector); ok {
			if len(sel.Parts) == 1 {
				selectVar = sel.Parts[0]
			}
		}
	}
	selectingEndNode := selectVar == endNode.Variable

	// Build a UNION ALL of JOIN chains for each hop depth from minHops to maxHops
	first := true
	for depth := minHops; depth <= maxHops; depth++ {
		if !first {
			sql.WriteString("\nUNION ALL\n")
		}
		first = false

		if selectingEndNode {
			sql.WriteString(fmt.Sprintf("SELECT n_end.*\nFROM edges e0\n"))
		} else {
			sql.WriteString(fmt.Sprintf("SELECT n_start.*\nFROM edges e0\n"))
		}

		// INDEXED BY hint on first edge
		if hasEdgeType {
			sql.WriteString("INDEXED BY idx_edges_from_type\n")
		}

		// JOIN start node with type filter
		sql.WriteString("JOIN nodes n_start ON n_start.id = e0.from_id")
		if startNode.Type != "" {
			sql.WriteString(" AND n_start.type = ?")
			args = append(args, startNode.Type)
		}
		sql.WriteString("\n")

		// Chain additional edge JOINs for hops > 1
		for i := 1; i < depth; i++ {
			prev := fmt.Sprintf("e%d", i-1)
			cur := fmt.Sprintf("e%d", i)
			if hasEdgeType {
				sql.WriteString(fmt.Sprintf("JOIN edges %s INDEXED BY idx_edges_from_type ON %s.from_id = %s.to_id", cur, cur, prev))
			} else {
				sql.WriteString(fmt.Sprintf("JOIN edges %s ON %s.from_id = %s.to_id", cur, cur, prev))
			}
			// Edge type on chained edges
			if len(edge.Types) > 1 {
				placeholders := make([]string, len(edge.Types))
				for j := range edge.Types {
					placeholders[j] = "?"
					args = append(args, edge.Types[j])
				}
				sql.WriteString(fmt.Sprintf(" AND %s.type IN (%s)", cur, strings.Join(placeholders, ", ")))
			} else if edge.Type != "" {
				sql.WriteString(fmt.Sprintf(" AND %s.type = ?", cur))
				args = append(args, edge.Type)
			}
			sql.WriteString("\n")
		}

		// JOIN end node
		lastEdge := fmt.Sprintf("e%d", depth-1)
		sql.WriteString(fmt.Sprintf("JOIN nodes n_end ON n_end.id = %s.to_id", lastEdge))
		if endNode.Type != "" {
			sql.WriteString(" AND n_end.type = ?")
			args = append(args, endNode.Type)
		}
		sql.WriteString("\n")

		// WHERE clause for first edge type
		if len(edge.Types) > 1 {
			placeholders := make([]string, len(edge.Types))
			for j := range edge.Types {
				placeholders[j] = "?"
				args = append(args, edge.Types[j])
			}
			sql.WriteString(fmt.Sprintf("WHERE e0.type IN (%s)\n", strings.Join(placeholders, ", ")))
		} else if edge.Type != "" {
			sql.WriteString("WHERE e0.type = ?\n")
			args = append(args, edge.Type)
		}
	}

	// Add LIMIT if present
	if q.Select.Limit != nil {
		sql.WriteString("LIMIT ?")
		args = append(args, *q.Select.Limit)
	}

	return sql.String(), args, graph.ResultTypeNodes, nil
}

// compileCTEVariableLength generates a recursive CTE for unbounded or very deep
// variable-length paths. Uses INDEXED BY hints and pushes start node type into
// the CTE base case for better performance.
func (s *Storage) compileCTEVariableLength(q *aql.Query, startNode *aql.NodePattern, edge *aql.EdgePattern, endNode *aql.NodePattern, minHops, maxHops int) (string, []any, graph.ResultType, error) {
	var args []any
	var sql strings.Builder
	hasEdgeType := edge.Type != "" || len(edge.Types) > 0

	// Determine which node to select
	selectVar := ""
	if len(q.Select.Columns) > 0 {
		if sel, ok := q.Select.Columns[0].Expr.(*aql.Selector); ok {
			if len(sel.Parts) == 1 {
				selectVar = sel.Parts[0]
			}
		}
	}
	selectingEndNode := selectVar == endNode.Variable
	startTypePushed := startNode.Type != "" && hasEdgeType
	trackStartID := !selectingEndNode || !startTypePushed

	if trackStartID {
		sql.WriteString("WITH RECURSIVE paths(start_id, node_id, depth) AS (\n")
	} else {
		sql.WriteString("WITH RECURSIVE paths(node_id, depth) AS (\n")
	}

	// Base case
	if trackStartID {
		sql.WriteString("  SELECT e.from_id, e.to_id, 1\n")
	} else {
		sql.WriteString("  SELECT e.to_id, 1\n")
	}
	sql.WriteString("  FROM edges e\n")

	if startTypePushed {
		sql.WriteString("  INDEXED BY idx_edges_from_type\n")
		sql.WriteString("  JOIN nodes n_start ON n_start.id = e.from_id AND n_start.type = ?\n")
		args = append(args, startNode.Type)
	}

	if len(edge.Types) > 1 {
		placeholders := make([]string, len(edge.Types))
		for i := range edge.Types {
			placeholders[i] = "?"
			args = append(args, edge.Types[i])
		}
		sql.WriteString(fmt.Sprintf("  WHERE e.type IN (%s)\n", strings.Join(placeholders, ", ")))
	} else if edge.Type != "" {
		sql.WriteString("  WHERE e.type = ?\n")
		args = append(args, edge.Type)
	}

	sql.WriteString("\n  UNION ALL\n\n")

	// Recursive case
	if trackStartID {
		sql.WriteString("  SELECT p.start_id, e.to_id, p.depth + 1\n")
	} else {
		sql.WriteString("  SELECT e.to_id, p.depth + 1\n")
	}
	sql.WriteString("  FROM paths p\n")
	if hasEdgeType {
		sql.WriteString("  JOIN edges e INDEXED BY idx_edges_from_type\n")
		sql.WriteString("    ON e.from_id = p.node_id")
	} else {
		sql.WriteString("  JOIN edges e ON e.from_id = p.node_id")
	}

	var recursiveWhere []string
	if len(edge.Types) > 1 {
		placeholders := make([]string, len(edge.Types))
		for i := range edge.Types {
			placeholders[i] = "?"
			args = append(args, edge.Types[i])
		}
		recursiveWhere = append(recursiveWhere, fmt.Sprintf("e.type IN (%s)", strings.Join(placeholders, ", ")))
	} else if edge.Type != "" {
		sql.WriteString(" AND e.type = ?")
		args = append(args, edge.Type)
	}
	sql.WriteString("\n")

	if maxHops > 0 {
		recursiveWhere = append(recursiveWhere, fmt.Sprintf("p.depth < %d", maxHops))
	}

	if len(recursiveWhere) > 0 {
		sql.WriteString("  WHERE " + strings.Join(recursiveWhere, " AND ") + "\n")
	}

	sql.WriteString(")\n")

	// Outer query
	if selectingEndNode && !trackStartID {
		sql.WriteString("SELECT DISTINCT n1.*\n")
		sql.WriteString("FROM paths p\n")
		sql.WriteString("JOIN nodes n1 ON n1.id = p.node_id\n")
	} else if selectingEndNode {
		sql.WriteString("SELECT DISTINCT n1.*\n")
		sql.WriteString("FROM nodes n0\n")
		sql.WriteString("JOIN paths p ON p.start_id = n0.id\n")
		sql.WriteString("JOIN nodes n1 ON n1.id = p.node_id\n")
	} else {
		sql.WriteString("SELECT DISTINCT n0.*\n")
		sql.WriteString("FROM nodes n0\n")
		sql.WriteString("JOIN paths p ON p.start_id = n0.id\n")
		sql.WriteString("JOIN nodes n1 ON n1.id = p.node_id\n")
	}

	var whereClauses []string
	if minHops > 1 || maxHops > 0 {
		if minHops > 1 {
			whereClauses = append(whereClauses, fmt.Sprintf("p.depth >= %d", minHops))
		}
		if maxHops > 0 {
			whereClauses = append(whereClauses, fmt.Sprintf("p.depth <= %d", maxHops))
		}
	}

	if startNode.Type != "" && !startTypePushed {
		whereClauses = append(whereClauses, "n0.type = ?")
		args = append(args, startNode.Type)
	}
	if endNode.Type != "" {
		whereClauses = append(whereClauses, "n1.type = ?")
		args = append(args, endNode.Type)
	}

	if len(whereClauses) > 0 {
		sql.WriteString("WHERE " + strings.Join(whereClauses, " AND ") + "\n")
	}

	if q.Select.Limit != nil {
		sql.WriteString("LIMIT ?")
		args = append(args, *q.Select.Limit)
	}

	return sql.String(), args, graph.ResultTypeNodes, nil
}

// compilePatternSelect compiles the SELECT clause for pattern queries.
func (s *Storage) compilePatternSelect(stmt *aql.SelectStmt, nodeAliases map[string]string, edgeAliases map[string]string) (string, error) {
	var sqlBuilder strings.Builder
	sqlBuilder.WriteString("SELECT")

	if stmt.Distinct {
		sqlBuilder.WriteString(" DISTINCT")
	}

	// SELECT can include node variables, edge variables, or field selectors
	// For now, support simple variable selectors only
	for i, col := range stmt.Columns {
		if i > 0 {
			sqlBuilder.WriteString(",")
		}
		sqlBuilder.WriteString(" ")

		// Check if this is a variable selector
		if sel, ok := col.Expr.(*aql.Selector); ok {
			if len(sel.Parts) == 1 {
				// Simple variable: SELECT file or SELECT e
				varName := sel.Parts[0]

				// Check if it's a node variable
				if alias, ok := nodeAliases[varName]; ok {
					// Select all columns from this node
					sqlBuilder.WriteString(alias)
					sqlBuilder.WriteString(".*")
					continue
				}

				// Check if it's an edge variable
				if alias, ok := edgeAliases[varName]; ok {
					// Select all columns from this edge
					sqlBuilder.WriteString(alias)
					sqlBuilder.WriteString(".*")
					continue
				}

				return "", fmt.Errorf("undefined variable: %s", varName)
			} else if len(sel.Parts) > 1 {
				// Field selector: SELECT file.name or SELECT file.data.ext
				resolved, err := s.resolvePatternSelector(sel, nodeAliases, edgeAliases)
				if err != nil {
					return "", err
				}
				sqlBuilder.WriteString(resolved)
				continue
			}
		}

		// Handle COUNT(*)
		if _, ok := col.Expr.(*aql.CountCall); ok {
			sqlBuilder.WriteString("COUNT(*)")
			continue
		}

		return "", fmt.Errorf("unsupported SELECT expression in pattern: %T", col.Expr)
	}

	return sqlBuilder.String(), nil
}

// compilePatternWhere compiles WHERE clause for pattern queries with variable references.
func (s *Storage) compilePatternWhere(expr aql.Expression, nodeAliases map[string]string, edgeAliases map[string]string) (string, []any, error) {
	switch e := expr.(type) {
	case *aql.BinaryExpr:
		return s.compilePatternBinaryExpr(e, nodeAliases, edgeAliases)
	case *aql.UnaryExpr:
		inner, args, err := s.compilePatternWhere(e.Operand, nodeAliases, edgeAliases)
		if err != nil {
			return "", nil, err
		}
		return "NOT (" + inner + ")", args, nil
	case *aql.ComparisonExpr:
		return s.compilePatternComparison(e, nodeAliases, edgeAliases)
	case *aql.InExpr:
		return s.compilePatternIn(e, nodeAliases, edgeAliases)
	case *aql.BetweenExpr:
		return s.compilePatternBetween(e, nodeAliases, edgeAliases)
	case *aql.IsNullExpr:
		return s.compilePatternIsNull(e, nodeAliases, edgeAliases)
	case *aql.ParenExpr:
		inner, args, err := s.compilePatternWhere(e.Inner, nodeAliases, edgeAliases)
		if err != nil {
			return "", nil, err
		}
		return "(" + inner + ")", args, nil
	default:
		return "", nil, fmt.Errorf("unsupported expression in pattern WHERE: %T", expr)
	}
}

func (s *Storage) compilePatternBinaryExpr(e *aql.BinaryExpr, nodeAliases map[string]string, edgeAliases map[string]string) (string, []any, error) {
	left, leftArgs, err := s.compilePatternWhere(e.Left, nodeAliases, edgeAliases)
	if err != nil {
		return "", nil, err
	}
	right, rightArgs, err := s.compilePatternWhere(e.Right, nodeAliases, edgeAliases)
	if err != nil {
		return "", nil, err
	}

	op := ""
	switch e.Op {
	case aql.OpAnd:
		op = " AND "
	case aql.OpOr:
		op = " OR "
	default:
		return "", nil, fmt.Errorf("unsupported binary operator: %v", e.Op)
	}

	sql := left + op + right
	args := append(leftArgs, rightArgs...)
	return sql, args, nil
}

func (s *Storage) compilePatternComparison(e *aql.ComparisonExpr, nodeAliases map[string]string, edgeAliases map[string]string) (string, []any, error) {
	// Resolve the selector (variable.field)
	field, err := s.resolvePatternSelector(e.Left, nodeAliases, edgeAliases)
	if err != nil {
		return "", nil, err
	}

	value, err := s.compileValue(e.Right)
	if err != nil {
		return "", nil, err
	}

	op := ""
	switch e.Op {
	case aql.OpEq:
		op = " = "
	case aql.OpNe:
		op = " != "
	case aql.OpLt:
		op = " < "
	case aql.OpLe:
		op = " <= "
	case aql.OpGt:
		op = " > "
	case aql.OpGe:
		op = " >= "
	case aql.OpLike:
		op = " LIKE "
	case aql.OpGlob:
		op = " GLOB "
	case aql.OpNotLike:
		op = " NOT LIKE "
	case aql.OpNotGlob:
		op = " NOT GLOB "
	default:
		return "", nil, fmt.Errorf("unsupported comparison operator: %v", e.Op)
	}

	sql := field + op + "?"
	return sql, []any{value}, nil
}

func (s *Storage) compilePatternIn(e *aql.InExpr, nodeAliases map[string]string, edgeAliases map[string]string) (string, []any, error) {
	field, err := s.resolvePatternSelector(e.Left, nodeAliases, edgeAliases)
	if err != nil {
		return "", nil, err
	}

	op := "IN"
	if e.Not {
		op = "NOT IN"
	}

	// Subquery: IN (SELECT ...)
	if e.Subquery != nil {
		innerSQL, innerArgs, _, err := s.compileToSQL(e.Subquery)
		if err != nil {
			return "", nil, fmt.Errorf("subquery in %s: %w", op, err)
		}
		return fmt.Sprintf("%s %s (%s)", field, op, innerSQL), innerArgs, nil
	}

	var args []any
	placeholders := make([]string, len(e.Values))
	for i, v := range e.Values {
		val, err := s.compileValue(v)
		if err != nil {
			return "", nil, err
		}
		placeholders[i] = "?"
		args = append(args, val)
	}

	return fmt.Sprintf("%s %s (%s)", field, op, strings.Join(placeholders, ", ")), args, nil
}
func (s *Storage) compilePatternBetween(e *aql.BetweenExpr, nodeAliases map[string]string, edgeAliases map[string]string) (string, []any, error) {
	field, err := s.resolvePatternSelector(e.Left, nodeAliases, edgeAliases)
	if err != nil {
		return "", nil, err
	}

	low, err := s.compileValue(e.Low)
	if err != nil {
		return "", nil, err
	}

	high, err := s.compileValue(e.High)
	if err != nil {
		return "", nil, err
	}

	sql := fmt.Sprintf("%s BETWEEN ? AND ?", field)
	return sql, []any{low, high}, nil
}

func (s *Storage) compilePatternIsNull(e *aql.IsNullExpr, nodeAliases map[string]string, edgeAliases map[string]string) (string, []any, error) {
	field, err := s.resolvePatternSelector(e.Selector, nodeAliases, edgeAliases)
	if err != nil {
		return "", nil, err
	}

	if e.Not {
		return field + " IS NOT NULL", nil, nil
	}
	return field + " IS NULL", nil, nil
}

// resolvePatternSelector converts a selector like "branch.name" to "n0.name" using aliases.
func (s *Storage) resolvePatternSelector(sel *aql.Selector, nodeAliases map[string]string, edgeAliases map[string]string) (string, error) {
	if len(sel.Parts) == 0 {
		return "", fmt.Errorf("empty selector")
	}

	varName := sel.Parts[0]

	// Check if it's a node variable
	if alias, ok := nodeAliases[varName]; ok {
		if len(sel.Parts) == 1 {
			// Just the variable, no field - error
			return "", fmt.Errorf("must specify field: %s.fieldname", varName)
		}

		// Handle field access (e.g., branch.name)
		if len(sel.Parts) == 2 {
			return alias + "." + sel.Parts[1], nil
		}

		// Handle JSON field access (e.g., branch.data.commit)
		if sel.Parts[1] == "data" && len(sel.Parts) > 2 {
			jsonPath := "$." + strings.Join(sel.Parts[2:], ".")
			return fmt.Sprintf("json_extract(%s.data, '%s')", alias, jsonPath), nil
		}

		return "", fmt.Errorf("invalid selector: %v", sel.Parts)
	}

	// Check if it's an edge variable
	if alias, ok := edgeAliases[varName]; ok {
		if len(sel.Parts) == 1 {
			return "", fmt.Errorf("must specify field: %s.fieldname", varName)
		}

		// Handle field access (e.g., e.type)
		if len(sel.Parts) == 2 {
			return alias + "." + sel.Parts[1], nil
		}

		return "", fmt.Errorf("invalid selector: %v", sel.Parts)
	}

	return "", fmt.Errorf("undefined variable: %s", varName)
}

// compileSelect compiles the SELECT clause.
func (s *Storage) compileSelect(stmt *aql.SelectStmt, table string) (string, error) {
	var sqlBuilder strings.Builder
	sqlBuilder.WriteString("SELECT")

	if stmt.Distinct {
		sqlBuilder.WriteString(" DISTINCT")
	}

	// Check for GROUP BY to validate * usage
	hasGroupBy := len(stmt.GroupBy) > 0

	for i, col := range stmt.Columns {
		if i > 0 {
			sqlBuilder.WriteString(",")
		}
		sqlBuilder.WriteString(" ")

		switch expr := col.Expr.(type) {
		case *aql.Star:
			if hasGroupBy {
				return "", fmt.Errorf("cannot use * with GROUP BY")
			}
			sqlBuilder.WriteString(s.allColumnsForTable(table))

		case *aql.CountCall:
			sqlBuilder.WriteString("COUNT(*)")

		case *aql.Selector:
			field, _ := s.compileSelectorToSQL(expr, table)
			sqlBuilder.WriteString(field)

		default:
			return "", fmt.Errorf("unsupported column expression: %T", expr)
		}

		// Alias
		if col.Alias != "" {
			sqlBuilder.WriteString(" AS ")
			sqlBuilder.WriteString(col.Alias)
		}
	}

	return sqlBuilder.String(), nil
}

// allColumnsForTable returns the column list for SELECT *.
func (s *Storage) allColumnsForTable(table string) string {
	switch table {
	case "nodes":
		return "id, type, uri, key, name, labels, data, generation, root, created_at, updated_at"
	case "edges":
		return "id, type, from_id, to_id, data, generation, created_at"
	default:
		return "*"
	}
}

// allColumnsWithPrefix returns the column list with table prefix for CTE+JOIN queries.
func (s *Storage) allColumnsWithPrefix(table string) string {
	switch table {
	case "nodes":
		return "nodes.id, nodes.type, nodes.uri, nodes.key, nodes.name, nodes.labels, nodes.data, nodes.generation, nodes.root, nodes.created_at, nodes.updated_at"
	case "edges":
		return "edges.id, edges.type, edges.from_id, edges.to_id, edges.data, edges.generation, edges.created_at"
	default:
		return table + ".*"
	}
}

// compileSelectWithPrefix compiles SELECT with explicit table prefix for all columns.
// Used when FROM clause is CTE+JOIN to avoid ambiguous column references.
func (s *Storage) compileSelectWithPrefix(stmt *aql.SelectStmt, table string) (string, error) {
	var sqlBuilder strings.Builder
	sqlBuilder.WriteString("SELECT")

	if stmt.Distinct {
		sqlBuilder.WriteString(" DISTINCT")
	}

	// Check for GROUP BY to validate * usage
	hasGroupBy := len(stmt.GroupBy) > 0

	for i, col := range stmt.Columns {
		if i > 0 {
			sqlBuilder.WriteString(",")
		}
		sqlBuilder.WriteString(" ")

		switch expr := col.Expr.(type) {
		case *aql.Star:
			if hasGroupBy {
				return "", fmt.Errorf("cannot use * with GROUP BY")
			}
			sqlBuilder.WriteString(s.allColumnsWithPrefix(table))

		case *aql.CountCall:
			sqlBuilder.WriteString("COUNT(*)")

		case *aql.Selector:
			field, _ := s.compileSelectorToSQL(expr, table)
			sqlBuilder.WriteString(field)

		default:
			return "", fmt.Errorf("unsupported column expression: %T", expr)
		}

		// Alias
		if col.Alias != "" {
			sqlBuilder.WriteString(" AS ")
			sqlBuilder.WriteString(col.Alias)
		}
	}

	return sqlBuilder.String(), nil
}

// compileWhere compiles an expression to SQL WHERE clause.
func (s *Storage) compileWhere(expr aql.Expression, table string) (string, []any, error) {
	switch e := expr.(type) {
	case *aql.BinaryExpr:
		return s.compileBinaryExpr(e, table)
	case *aql.UnaryExpr:
		return s.compileUnaryExpr(e, table)
	case *aql.ComparisonExpr:
		return s.compileComparison(e, table)
	case *aql.InExpr:
		return s.compileIn(e, table)
	case *aql.BetweenExpr:
		return s.compileBetween(e, table)
	case *aql.LabelExpr:
		return s.compileLabel(e, table)
	case *aql.IsNullExpr:
		return s.compileIsNull(e, table)
	case *aql.ExistsExpr:
		return s.compileExists(e, table)
	case *aql.ParenExpr:
		inner, args, err := s.compileWhere(e.Inner, table)
		if err != nil {
			return "", nil, err
		}
		return "(" + inner + ")", args, nil
	default:
		return "", nil, fmt.Errorf("unsupported expression type: %T", expr)
	}
}

// compileBinaryExpr compiles AND/OR expressions.
func (s *Storage) compileBinaryExpr(e *aql.BinaryExpr, table string) (string, []any, error) {
	left, leftArgs, err := s.compileWhere(e.Left, table)
	if err != nil {
		return "", nil, err
	}

	right, rightArgs, err := s.compileWhere(e.Right, table)
	if err != nil {
		return "", nil, err
	}

	var op string
	switch e.Op {
	case aql.OpAnd:
		op = "AND"
	case aql.OpOr:
		op = "OR"
	default:
		return "", nil, fmt.Errorf("unsupported binary operator: %v", e.Op)
	}

	sql := fmt.Sprintf("%s %s %s", left, op, right)
	args := append(leftArgs, rightArgs...)
	return sql, args, nil
}

// compileUnaryExpr compiles NOT expressions.
func (s *Storage) compileUnaryExpr(e *aql.UnaryExpr, table string) (string, []any, error) {
	operand, args, err := s.compileWhere(e.Operand, table)
	if err != nil {
		return "", nil, err
	}

	if e.Op != aql.OpNot {
		return "", nil, fmt.Errorf("unsupported unary operator: %v", e.Op)
	}

	return "NOT " + operand, args, nil
}

// compileComparison compiles field = value comparisons.
func (s *Storage) compileComparison(e *aql.ComparisonExpr, table string) (string, []any, error) {
	// Handle selector (may be JSON path like data.ext)
	field, _ := s.compileSelectorToSQL(e.Left, table)

	// Get comparison operator
	op := s.compileComparisonOp(e.Op)

	// Get value
	value, err := s.compileValue(e.Right)
	if err != nil {
		return "", nil, err
	}

	sql := fmt.Sprintf("%s %s ?", field, op)
	return sql, []any{value}, nil
}

// compileComparisonOp converts AQL comparison operator to SQL.
func (s *Storage) compileComparisonOp(op aql.ComparisonOp) string {
	switch op {
	case aql.OpEq:
		return "="
	case aql.OpNe:
		return "!="
	case aql.OpLt:
		return "<"
	case aql.OpLe:
		return "<="
	case aql.OpGt:
		return ">"
	case aql.OpGe:
		return ">="
	case aql.OpLike:
		return "LIKE"
	case aql.OpGlob:
		return "GLOB"
	case aql.OpNotLike:
		return "NOT LIKE"
	case aql.OpNotGlob:
		return "NOT GLOB"
	default:
		return "="
	}
}

// compileSelectorToSQL converts selector to SQL column reference.
// Returns (sql string, isJSON bool).
func (s *Storage) compileSelectorToSQL(sel *aql.Selector, table string) (string, bool) {
	if len(sel.Parts) == 0 {
		return "", false
	}

	// Single part: direct column
	if len(sel.Parts) == 1 {
		return sel.Parts[0], false
	}

	// Multiple parts: JSON path (e.g., data.ext → json_extract(data, '$.ext'))
	if sel.Parts[0] == "data" {
		jsonPath := "$." + strings.Join(sel.Parts[1:], ".")
		return fmt.Sprintf("json_extract(data, '%s')", jsonPath), true
	}

	// Other multi-part selectors (e.g., node.name in patterns - Phase 2)
	return strings.Join(sel.Parts, "."), false
}

// compileValue converts AQL value to Go value for SQL parameter.
func (s *Storage) compileValue(v aql.Value) (any, error) {
	switch val := v.(type) {
	case *aql.StringLit:
		return val.Value, nil
	case *aql.NumberLit:
		if val.IsInt {
			return val.IntValue(), nil
		}
		return val.Value, nil
	case *aql.BoolLit:
		return val.Value, nil
	case *aql.Parameter:
		return nil, fmt.Errorf("parameters not yet supported (will be added in Phase 2)")
	default:
		return nil, fmt.Errorf("unsupported value type: %T", v)
	}
}

// compileIn compiles IN / NOT IN expressions, including IN (SELECT ...) subqueries.
func (s *Storage) compileIn(e *aql.InExpr, table string) (string, []any, error) {
	field, _ := s.compileSelectorToSQL(e.Left, table)

	op := "IN"
	if e.Not {
		op = "NOT IN"
	}

	// Subquery: IN (SELECT ...)
	if e.Subquery != nil {
		innerSQL, innerArgs, _, err := s.compileToSQL(e.Subquery)
		if err != nil {
			return "", nil, fmt.Errorf("subquery in %s: %w", op, err)
		}
		return fmt.Sprintf("%s %s (%s)", field, op, innerSQL), innerArgs, nil
	}

	// Literal value list
	var args []any
	placeholders := make([]string, len(e.Values))
	for i, v := range e.Values {
		val, err := s.compileValue(v)
		if err != nil {
			return "", nil, err
		}
		placeholders[i] = "?"
		args = append(args, val)
	}

	return fmt.Sprintf("%s %s (%s)", field, op, strings.Join(placeholders, ", ")), args, nil
}

// compileBetween compiles BETWEEN expressions.
func (s *Storage) compileBetween(e *aql.BetweenExpr, table string) (string, []any, error) {
	field, _ := s.compileSelectorToSQL(e.Left, table)

	low, err := s.compileValue(e.Low)
	if err != nil {
		return "", nil, err
	}

	high, err := s.compileValue(e.High)
	if err != nil {
		return "", nil, err
	}

	sql := fmt.Sprintf("%s BETWEEN ? AND ?", field)
	return sql, []any{low, high}, nil
}

// compileLabel compiles label operations (CONTAINS ANY/ALL, NOT CONTAINS).
func (s *Storage) compileLabel(e *aql.LabelExpr, table string) (string, []any, error) {
	if table != "nodes" {
		return "", nil, fmt.Errorf("label operations only supported on nodes table")
	}

	var conditions []string
	var args []any

	for _, labelVal := range e.Labels {
		str, ok := labelVal.(*aql.StringLit)
		if !ok {
			return "", nil, fmt.Errorf("label must be string literal, got %T", labelVal)
		}
		conditions = append(conditions, "labels LIKE ?")
		args = append(args, `%"`+str.Value+`"%`)
	}

	var sql string
	switch e.Op {
	case aql.OpContainsAny:
		sql = "(" + strings.Join(conditions, " OR ") + ")"
	case aql.OpContainsAll:
		sql = "(" + strings.Join(conditions, " AND ") + ")"
	case aql.OpNotContains:
		sql = "NOT (" + strings.Join(conditions, " OR ") + ")"
	default:
		return "", nil, fmt.Errorf("unsupported label operator: %v", e.Op)
	}

	return sql, args, nil
}

// compileIsNull compiles IS NULL / IS NOT NULL.
func (s *Storage) compileIsNull(e *aql.IsNullExpr, table string) (string, []any, error) {
	field, _ := s.compileSelectorToSQL(e.Selector, table)

	if e.Not {
		return field + " IS NOT NULL", nil, nil
	}
	return field + " IS NULL", nil, nil
}

// compileExists compiles EXISTS/NOT EXISTS expressions to SQL subqueries.
// Handles single-hop, multi-hop, and variable-length path patterns.
func (s *Storage) compileExists(e *aql.ExistsExpr, table string) (string, []any, error) {
	// Pattern must have elements (at minimum just a node, but typically node-edge-node)
	if len(e.Pattern.Elements) == 0 {
		return "", nil, fmt.Errorf("EXISTS pattern is empty")
	}

	// Check if this is a variable-length pattern
	hasVariableLength := false
	for _, elem := range e.Pattern.Elements {
		if ep, ok := elem.(*aql.EdgePattern); ok && ep.IsVariableLength() {
			hasVariableLength = true
			break
		}
	}

	var sql string
	var args []any
	var err error

	if hasVariableLength {
		// Use recursive CTE for variable-length paths
		sql, args, err = s.compileExistsRecursive(e, table)
	} else {
		// Use simple JOIN-based subquery
		sql, args, err = s.compileExistsSimple(e, table)
	}

	if err != nil {
		return "", nil, err
	}

	if e.Not {
		return "NOT " + sql, args, nil
	}
	return sql, args, nil
}

// compileExistsSimple compiles EXISTS for patterns without variable-length paths.
// Handles patterns like: (source)-[:edge]->(target)
func (s *Storage) compileExistsSimple(e *aql.ExistsExpr, table string) (string, []any, error) {
	var sql strings.Builder
	var args []any

	// Pattern must be: node, edge, node (minimum 3 elements)
	if len(e.Pattern.Elements) < 3 {
		return "", nil, fmt.Errorf("EXISTS pattern requires at least 3 elements (node-edge-node)")
	}

	// Get source node (must be a variable from outer scope)
	sourceNode, ok := e.Pattern.Elements[0].(*aql.NodePattern)
	if !ok {
		return "", nil, fmt.Errorf("EXISTS pattern must start with a node")
	}
	if sourceNode.Variable == "" {
		return "", nil, fmt.Errorf("EXISTS pattern must start with a variable from outer scope")
	}

	// Start building the subquery
	sql.WriteString("EXISTS (SELECT 1 FROM edges e JOIN nodes target ON ")

	// Get first edge pattern (element 1)
	edgePattern, ok := e.Pattern.Elements[1].(*aql.EdgePattern)
	if !ok {
		return "", nil, fmt.Errorf("expected edge pattern at position 1")
	}

	// Connect source to edge based on direction
	switch edgePattern.Direction {
	case aql.Outgoing:
		// (source)-[:edge]->(target)
		sql.WriteString(fmt.Sprintf("e.from_id = %s.id AND e.to_id = target.id", table))
	case aql.Incoming:
		// (source)<-[:edge]-(target)
		sql.WriteString(fmt.Sprintf("e.to_id = %s.id AND e.from_id = target.id", table))
	case aql.Undirected:
		// (source)-[:edge]-(target)
		sql.WriteString(fmt.Sprintf("((e.from_id = %s.id AND e.to_id = target.id) OR (e.to_id = %s.id AND e.from_id = target.id))",
			table, table))
	}

	// Add WHERE conditions
	var whereConditions []string

	// Add edge type constraint
	if edgePattern.Type != "" {
		whereConditions = append(whereConditions, "e.type = ?")
		args = append(args, edgePattern.Type)
	} else if len(edgePattern.Types) > 0 {
		placeholders := make([]string, len(edgePattern.Types))
		for i := range edgePattern.Types {
			placeholders[i] = "?"
			args = append(args, edgePattern.Types[i])
		}
		whereConditions = append(whereConditions, fmt.Sprintf("e.type IN (%s)", strings.Join(placeholders, ", ")))
	}

	// Get target node (element 2)
	targetNode, ok := e.Pattern.Elements[2].(*aql.NodePattern)
	if !ok {
		return "", nil, fmt.Errorf("expected node pattern at position 2")
	}

	// Add target node type constraint
	if targetNode.Type != "" {
		if strings.Contains(targetNode.Type, "*") || strings.Contains(targetNode.Type, "?") {
			whereConditions = append(whereConditions, "target.type GLOB ?")
			args = append(args, targetNode.Type)
		} else {
			whereConditions = append(whereConditions, "target.type = ?")
			args = append(args, targetNode.Type)
		}
	}

	// Add inline WHERE for target node
	if targetNode.Where != nil {
		whereSQL, whereArgs, err := s.compileWhere(targetNode.Where, "target")
		if err != nil {
			return "", nil, fmt.Errorf("compiling inline WHERE: %w", err)
		}
		whereConditions = append(whereConditions, whereSQL)
		args = append(args, whereArgs...)
	}

	// Add WHERE clause if we have conditions
	if len(whereConditions) > 0 {
		sql.WriteString(" WHERE ")
		sql.WriteString(strings.Join(whereConditions, " AND "))
	}

	sql.WriteString(")")

	return sql.String(), args, nil
}

// compileExistsRecursive compiles EXISTS for patterns with variable-length paths using recursive CTEs.
func (s *Storage) compileExistsRecursive(e *aql.ExistsExpr, table string) (string, []any, error) {
	// Find the variable-length edge
	var varLenEdge *aql.EdgePattern
	var varLenIdx int
	for i, elem := range e.Pattern.Elements {
		if ep, ok := elem.(*aql.EdgePattern); ok && ep.IsVariableLength() {
			varLenEdge = ep
			varLenIdx = i
			break
		}
	}

	if varLenEdge == nil {
		return "", nil, fmt.Errorf("no variable-length edge found")
	}

	// Get source and target nodes
	if varLenIdx == 0 || varLenIdx >= len(e.Pattern.Elements)-1 {
		return "", nil, fmt.Errorf("variable-length edge must be between two nodes")
	}

	sourceNode := e.Pattern.Elements[varLenIdx-1].(*aql.NodePattern)
	targetNode := e.Pattern.Elements[varLenIdx+1].(*aql.NodePattern)

	if sourceNode.Variable == "" {
		return "", nil, fmt.Errorf("source node must have a variable")
	}

	var sql strings.Builder
	var args []any

	// Build recursive CTE
	sql.WriteString("EXISTS (WITH RECURSIVE path(node_id, depth) AS (")

	// Base case: start from source node
	sql.WriteString("SELECT ")
	sql.WriteString(table)
	sql.WriteString(".id, 0 FROM ")
	sql.WriteString(table)

	// Add source node constraints (type and inline WHERE)
	hasSourceConstraint := false
	if sourceNode.Type != "" || sourceNode.Where != nil {
		sql.WriteString(" WHERE ")
		if sourceNode.Type != "" {
			sql.WriteString(table)
			sql.WriteString(".type = ?")
			args = append(args, sourceNode.Type)
			hasSourceConstraint = true
		}
		if sourceNode.Where != nil {
			if hasSourceConstraint {
				sql.WriteString(" AND ")
			}
			whereSQL, whereArgs, err := s.compileWhere(sourceNode.Where, table)
			if err != nil {
				return "", nil, fmt.Errorf("compiling source node inline WHERE in EXISTS: %w", err)
			}
			sql.WriteString(whereSQL)
			args = append(args, whereArgs...)
		}
	}

	// Recursive case: follow edges
	sql.WriteString(" UNION ALL SELECT ")

	switch varLenEdge.Direction {
	case aql.Outgoing:
		sql.WriteString("e.to_id")
	case aql.Incoming:
		sql.WriteString("e.from_id")
	default:
		return "", nil, fmt.Errorf("undirected variable-length paths not yet supported")
	}

	sql.WriteString(", path.depth + 1 FROM path JOIN edges e ON ")

	switch varLenEdge.Direction {
	case aql.Outgoing:
		sql.WriteString("e.from_id = path.node_id")
	case aql.Incoming:
		sql.WriteString("e.to_id = path.node_id")
	}

	// Add edge type constraint
	if varLenEdge.Type != "" {
		sql.WriteString(" AND e.type = ?")
		args = append(args, varLenEdge.Type)
	} else if len(varLenEdge.Types) > 0 {
		sql.WriteString(" AND e.type IN (")
		for i, t := range varLenEdge.Types {
			if i > 0 {
				sql.WriteString(", ")
			}
			sql.WriteString("?")
			args = append(args, t)
		}
		sql.WriteString(")")
	}

	// Add depth constraints
	if varLenEdge.MinHops != nil {
		sql.WriteString(fmt.Sprintf(" WHERE path.depth + 1 >= %d", *varLenEdge.MinHops))
		if varLenEdge.MaxHops != nil {
			sql.WriteString(fmt.Sprintf(" AND path.depth + 1 <= %d", *varLenEdge.MaxHops))
		}
	} else if varLenEdge.MaxHops != nil {
		sql.WriteString(fmt.Sprintf(" WHERE path.depth + 1 <= %d", *varLenEdge.MaxHops))
	}

	sql.WriteString(") SELECT 1 FROM path JOIN nodes target ON path.node_id = target.id WHERE ")

	// Correlate with outer query: target must match the outer table row
	// If target node has a variable that matches the outer table, add correlation
	if targetNode.Variable != "" && targetNode.Variable == "nodes" {
		sql.WriteString("target.id = ")
		sql.WriteString(table)
		sql.WriteString(".id AND ")
	}

	// Add minimum depth constraint
	if varLenEdge.MinHops != nil {
		sql.WriteString(fmt.Sprintf("path.depth >= %d AND ", *varLenEdge.MinHops))
	} else {
		sql.WriteString("path.depth >= 1 AND ")
	}

	// Add target node type constraint
	if targetNode.Type != "" {
		sql.WriteString("target.type = ?")
		args = append(args, targetNode.Type)
	} else {
		sql.WriteString("1=1")
	}

	// Add target node inline WHERE if present
	if targetNode.Where != nil {
		whereSQL, whereArgs, err := s.compileWhere(targetNode.Where, "target")
		if err != nil {
			return "", nil, fmt.Errorf("compiling inline WHERE in EXISTS: %w", err)
		}
		sql.WriteString(" AND ")
		sql.WriteString(whereSQL)
		args = append(args, whereArgs...)
	}

	sql.WriteString(")")

	return sql.String(), args, nil
}

// compileGroupBy compiles GROUP BY clause.
func (s *Storage) compileGroupBy(selectors []aql.Selector, table string) (string, error) {
	var parts []string
	for _, sel := range selectors {
		field, _ := s.compileSelectorToSQL(&sel, table)
		parts = append(parts, field)
	}
	return strings.Join(parts, ", "), nil
}

// compileOrderBy compiles ORDER BY clause.
func (s *Storage) compileOrderBy(specs []aql.OrderSpec, table string) (string, error) {
	var parts []string
	for _, spec := range specs {
		var field string
		switch expr := spec.Expr.(type) {
		case *aql.Selector:
			field, _ = s.compileSelectorToSQL(expr, table)
		case *aql.CountCall:
			field = "COUNT(*)"
		default:
			return "", fmt.Errorf("unsupported ORDER BY expression: %T", expr)
		}

		if spec.Descending {
			field += " DESC"
		} else {
			field += " ASC"
		}
		parts = append(parts, field)
	}
	return strings.Join(parts, ", "), nil
}

// compilePatternGroupBy compiles GROUP BY clause for pattern queries.
func (s *Storage) compilePatternGroupBy(selectors []aql.Selector, nodeAliases map[string]string, edgeAliases map[string]string) (string, error) {
	var parts []string
	for _, sel := range selectors {
		resolved, err := s.resolvePatternSelector(&sel, nodeAliases, edgeAliases)
		if err != nil {
			return "", err
		}
		parts = append(parts, resolved)
	}
	return strings.Join(parts, ", "), nil
}

// compilePatternOrderBy compiles ORDER BY clause for pattern queries.
func (s *Storage) compilePatternOrderBy(specs []aql.OrderSpec, nodeAliases map[string]string, edgeAliases map[string]string) (string, error) {
	var parts []string
	for _, spec := range specs {
		var field string
		switch expr := spec.Expr.(type) {
		case *aql.Selector:
			// Resolve selector using pattern variables
			resolved, err := s.resolvePatternSelector(expr, nodeAliases, edgeAliases)
			if err != nil {
				return "", err
			}
			field = resolved
		case *aql.CountCall:
			field = "COUNT(*)"
		default:
			return "", fmt.Errorf("unsupported ORDER BY expression in pattern: %T", expr)
		}

		if spec.Descending {
			field += " DESC"
		} else {
			field += " ASC"
		}
		parts = append(parts, field)
	}
	return strings.Join(parts, ", "), nil
}

// executeQuery executes compiled SQL and returns results.
func (s *Storage) executeQuery(ctx context.Context, query *aql.Query, sql string, args []any, resultType graph.ResultType) (*graph.QueryResult, error) {
	// Ensure all writes are flushed before reading
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}

	result := &graph.QueryResult{Type: resultType}

	switch resultType {
	case graph.ResultTypeNodes:
		nodes, err := s.executeNodeQuery(ctx, query, sql, args)
		if err != nil {
			return nil, err
		}
		result.Nodes = nodes
		// Populate SelectedColumns for non-star SELECT queries
		result.SelectedColumns = extractSelectedColumns(query)

	case graph.ResultTypeEdges:
		edges, err := s.executeEdgeQuery(ctx, query, sql, args)
		if err != nil {
			return nil, err
		}
		result.Edges = edges

	case graph.ResultTypeCounts:
		counts, err := s.executeCountQuery(ctx, query, sql, args)
		if err != nil {
			return nil, err
		}
		result.Counts = counts

	default:
		return nil, fmt.Errorf("unknown result type: %v", resultType)
	}

	return result, nil
}

// extractSelectedColumns returns the column names from a non-star SELECT clause,
// preserving their order. Returns nil for SELECT *.
func extractSelectedColumns(query *aql.Query) []string {
	if query.Select == nil || len(query.Select.Columns) == 0 {
		return nil
	}
	// SELECT * → nil
	if len(query.Select.Columns) == 1 {
		if _, ok := query.Select.Columns[0].Expr.(*aql.Star); ok {
			return nil
		}
	}
	var cols []string
	for _, col := range query.Select.Columns {
		switch expr := col.Expr.(type) {
		case *aql.Selector:
			if col.Alias != "" {
				cols = append(cols, col.Alias)
			} else {
				cols = append(cols, expr.String())
			}
		case *aql.CountCall:
			// COUNT(*) is part of ResultTypeCounts path, skip here
		}
	}
	return cols
}

// executeNodeQuery executes SQL returning nodes.
// Handles both SELECT * and partial column selection.
func (s *Storage) executeNodeQuery(ctx context.Context, query *aql.Query, sqlQuery string, args []any) ([]*graph.Node, error) {
	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Get column names from the result set
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	// Check if this is SELECT *
	isStar := false
	if query.Select != nil && len(query.Select.Columns) == 1 {
		if _, ok := query.Select.Columns[0].Expr.(*aql.Star); ok {
			isStar = true
		}
	}

	var nodes []*graph.Node
	for rows.Next() {
		if isStar {
			// SELECT * - scan all 11 columns in order: id, type, uri, key, name, labels, data, generation, root, created_at, updated_at
			var node graph.Node
			var labelsStr, createdAt, updatedAt string
			var uri, key, name, dataStr, generation, root sql.NullString

			err := rows.Scan(&node.ID, &node.Type, &uri, &key, &name, &labelsStr,
				&dataStr, &generation, &root, &createdAt, &updatedAt)
			if err != nil {
				return nil, err
			}

			node.URI = uri.String
			node.Key = key.String
			node.Name = name.String
			node.Generation = generation.String

			if labelsStr != "" && labelsStr != "[]" {
				if err := json.Unmarshal([]byte(labelsStr), &node.Labels); err != nil {
					return nil, err
				}
			}

			if dataStr.Valid && dataStr.String != "" {
				var data any
				if err := json.Unmarshal([]byte(dataStr.String), &data); err != nil {
					return nil, err
				}
				node.Data = data
			}

			if t, err2 := time.Parse(time.RFC3339, createdAt); err2 == nil {
				node.CreatedAt = &t
			}
			if t, err2 := time.Parse(time.RFC3339, updatedAt); err2 == nil {
				node.UpdatedAt = &t
			}
			nodes = append(nodes, &node)
		} else {
			// Partial columns - scan dynamically
			node, err := s.scanNodePartial(rows, cols)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
		}
	}

	return nodes, rows.Err()
}

// scanNodePartial scans a row with partial columns into a Node.
func (s *Storage) scanNodePartial(rows *sql.Rows, cols []string) (*graph.Node, error) {
	// Create scan targets for each column
	scanDest := make([]any, len(cols))

	for i := range cols {
		scanDest[i] = new(any)
	}

	if err := rows.Scan(scanDest...); err != nil {
		return nil, err
	}

	node := &graph.Node{}

	// Map scanned values to node fields
	for i, col := range cols {
		val := *(scanDest[i].(*any))
		if val == nil {
			continue
		}

		switch col {
		case "id":
			if str, ok := val.(string); ok {
				node.ID = str
			}
		case "type":
			if str, ok := val.(string); ok {
				node.Type = str
			}
		case "uri":
			if str, ok := val.(string); ok {
				node.URI = str
			}
		case "key":
			if str, ok := val.(string); ok {
				node.Key = str
			}
		case "name":
			if str, ok := val.(string); ok {
				node.Name = str
			}
		case "labels":
			if str, ok := val.(string); ok && str != "" && str != "[]" {
				if err := json.Unmarshal([]byte(str), &node.Labels); err != nil {
					return nil, err
				}
			}
		case "data":
			if str, ok := val.(string); ok && str != "" {
				var data any
				if err := json.Unmarshal([]byte(str), &data); err != nil {
					return nil, err
				}
				node.Data = data
			}
		case "generation":
			if str, ok := val.(string); ok {
				node.Generation = str
			}
		case "created_at":
			if str, ok := val.(string); ok {
				if t, err2 := time.Parse(time.RFC3339, str); err2 == nil {
					node.CreatedAt = &t
				}
			}
		case "updated_at":
			if str, ok := val.(string); ok {
				if t, err2 := time.Parse(time.RFC3339, str); err2 == nil {
					node.UpdatedAt = &t
				}
			}
		default:
			// Handle json_extract columns: json_extract(data, '$.field')
			// Extract the field name and store in Data
			if strings.HasPrefix(col, "json_extract") {
				if str, ok := val.(string); ok && str != "" {
					// Parse the JSON path from column name: json_extract(data, '$.name') -> name
					if match := jsonExtractRegex.FindStringSubmatch(col); match != nil {
						fieldName := match[1]
						if node.Data == nil {
							node.Data = make(map[string]any)
						}
						if dataMap, ok := node.Data.(map[string]any); ok {
							dataMap[fieldName] = str
						}
					}
				}
			}
			// Note: 'root' column exists in DB but not mapped to Node struct
		}
	}

	return node, nil
}

// executeEdgeQuery executes SQL returning edges.
// Handles both SELECT * and partial column selection.
func (s *Storage) executeEdgeQuery(ctx context.Context, query *aql.Query, sqlQuery string, args []any) ([]*graph.Edge, error) {
	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Get column names from the result set
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	// Check if this is SELECT *
	isStar := false
	if query.Select != nil && len(query.Select.Columns) == 1 {
		if _, ok := query.Select.Columns[0].Expr.(*aql.Star); ok {
			isStar = true
		}
	}

	var edges []*graph.Edge
	for rows.Next() {
		if isStar {
			// SELECT * - scan all 7 columns in order: id, type, from_id, to_id, data, generation, created_at
			var edge graph.Edge
			var createdAt string
			var dataStr, generation sql.NullString

			err := rows.Scan(&edge.ID, &edge.Type, &edge.From, &edge.To,
				&dataStr, &generation, &createdAt)
			if err != nil {
				return nil, err
			}

			edge.Generation = generation.String

			if dataStr.Valid && dataStr.String != "" {
				var data any
				if err := json.Unmarshal([]byte(dataStr.String), &data); err != nil {
					return nil, err
				}
				edge.Data = data
			}

			if t, err2 := time.Parse(time.RFC3339, createdAt); err2 == nil {
				edge.CreatedAt = &t
			}
			edges = append(edges, &edge)
		} else {
			// Partial columns - scan dynamically
			edge, err := s.scanEdgePartial(rows, cols)
			if err != nil {
				return nil, err
			}
			edges = append(edges, edge)
		}
	}

	return edges, rows.Err()
}

// scanEdgePartial scans a row with partial columns into an Edge.
func (s *Storage) scanEdgePartial(rows *sql.Rows, cols []string) (*graph.Edge, error) {
	// Create scan targets for each column
	scanDest := make([]any, len(cols))
	values := make(map[string]any)

	for i, col := range cols {
		var v any
		scanDest[i] = &v
		values[col] = &v
	}

	if err := rows.Scan(scanDest...); err != nil {
		return nil, err
	}

	edge := &graph.Edge{}

	// Map scanned values to edge fields
	for col, ptr := range values {
		val := *(ptr.(*any))
		if val == nil {
			continue
		}

		switch col {
		case "id":
			if str, ok := val.(string); ok {
				edge.ID = str
			}
		case "type":
			if str, ok := val.(string); ok {
				edge.Type = str
			}
		case "from_id":
			if str, ok := val.(string); ok {
				edge.From = str
			}
		case "to_id":
			if str, ok := val.(string); ok {
				edge.To = str
			}
		case "data":
			if str, ok := val.(string); ok && str != "" {
				var data any
				if err := json.Unmarshal([]byte(str), &data); err != nil {
					return nil, err
				}
				edge.Data = data
			}
		case "generation":
			if str, ok := val.(string); ok {
				edge.Generation = str
			}
		case "created_at":
			if str, ok := val.(string); ok {
				if t, err2 := time.Parse(time.RFC3339, str); err2 == nil {
					edge.CreatedAt = &t
				}
			}
		}
	}

	return edge, nil
}

// executeCountQuery executes GROUP BY COUNT(*) queries.
func (s *Storage) executeCountQuery(ctx context.Context, query *aql.Query, sqlQuery string, args []any) ([]graph.CountItem, error) {
	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var counts []graph.CountItem
	hasGroupBy := len(query.Select.GroupBy) > 0

	for rows.Next() {
		if hasGroupBy {
			// GROUP BY query: scan (key, count)
			// Use NullString to handle NULL values from json_each on empty arrays
			var key sql.NullString
			var count int
			if err := rows.Scan(&key, &count); err != nil {
				return nil, err
			}
			// Skip NULL keys (from empty JSON arrays)
			if key.Valid {
				counts = append(counts, graph.CountItem{Name: key.String, Count: count})
			}
		} else {
			// Scalar COUNT(*): scan single integer
			var count int
			if err := rows.Scan(&count); err != nil {
				return nil, err
			}
			counts = append(counts, graph.CountItem{Name: "_count", Count: count})
		}
	}

	return counts, rows.Err()
}
