package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/codewandler/axon/aql"
	"github.com/codewandler/axon/graph"
)

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
	// Phase 1: Only flat queries
	switch src := q.Select.From.(type) {
	case *aql.TableSource:
		return s.compileFlatQuery(q)
	case *aql.PatternSource:
		return "", nil, 0, fmt.Errorf("pattern queries not yet supported (Phase 2)")
	default:
		return "", nil, 0, fmt.Errorf("unknown source type: %T", src)
	}
}

// compileFlatQuery compiles SELECT from nodes/edges table.
func (s *Storage) compileFlatQuery(q *aql.Query) (string, []any, graph.ResultType, error) {
	src := q.Select.From.(*aql.TableSource)

	// Validate table
	if src.Table != "nodes" && src.Table != "edges" {
		return "", nil, 0, fmt.Errorf("invalid table: %s (must be 'nodes' or 'edges')", src.Table)
	}

	// Determine result type
	hasGroupBy := len(q.Select.GroupBy) > 0
	hasCount := false
	for _, col := range q.Select.Columns {
		if _, ok := col.Expr.(*aql.CountCall); ok {
			hasCount = true
			break
		}
	}

	var resultType graph.ResultType
	if hasCount && hasGroupBy {
		resultType = graph.ResultTypeCounts
	} else if src.Table == "nodes" {
		resultType = graph.ResultTypeNodes
	} else {
		resultType = graph.ResultTypeEdges
	}

	var sqlBuilder strings.Builder
	var args []any

	// SELECT clause
	selectSQL, err := s.compileSelect(q.Select, src.Table)
	if err != nil {
		return "", nil, 0, err
	}
	sqlBuilder.WriteString(selectSQL)

	// FROM clause
	sqlBuilder.WriteString(" FROM ")
	sqlBuilder.WriteString(src.Table)

	// WHERE clause
	if q.Select.Where != nil {
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
		return "", nil, fmt.Errorf("EXISTS not supported in Phase 1 (will be added in Phase 3)")
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

// compileIn compiles IN expressions.
func (s *Storage) compileIn(e *aql.InExpr, table string) (string, []any, error) {
	field, _ := s.compileSelectorToSQL(e.Left, table)

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

	sql := fmt.Sprintf("%s IN (%s)", field, strings.Join(placeholders, ", "))
	return sql, args, nil
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

			node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
			node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
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
				node.CreatedAt, _ = time.Parse(time.RFC3339, str)
			}
		case "updated_at":
			if str, ok := val.(string); ok {
				node.UpdatedAt, _ = time.Parse(time.RFC3339, str)
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

			edge.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
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
				edge.CreatedAt, _ = time.Parse(time.RFC3339, str)
			}
		}
	}

	return edge, nil
}

// executeCountQuery executes GROUP BY COUNT(*) queries.
func (s *Storage) executeCountQuery(ctx context.Context, query *aql.Query, sqlQuery string, args []any) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)

	for rows.Next() {
		var key string
		var count int

		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}

		counts[key] = count
	}

	return counts, rows.Err()
}
