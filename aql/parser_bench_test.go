package aql

import "testing"

var benchParser = NewParser()

func BenchmarkParser_SimpleQuery(b *testing.B) {
	query := "SELECT * FROM nodes WHERE type = 'fs:file'"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchParser.Parse(query)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParser_ComplexWhere(b *testing.B) {
	query := `SELECT * FROM nodes WHERE (type = 'fs:file' OR type = 'fs:dir') AND labels CONTAINS ANY ('important', 'reviewed') AND data.size > 1000`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchParser.Parse(query)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParser_PatternQuery(b *testing.B) {
	query := `SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file) WHERE file.data.ext = 'go'`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchParser.Parse(query)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParser_MultiPattern(b *testing.B) {
	query := `SELECT repo, branch, doc FROM (repo:vcs:repo)-[:has]->(branch:vcs:branch), (repo)-[:located_at]->(dir:fs:dir)-[:contains]->(doc:md:document) WHERE branch.name = 'main'`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchParser.Parse(query)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParser_ExistsSubquery(b *testing.B) {
	query := `SELECT dir FROM (dir:fs:dir) WHERE EXISTS (dir)-[:contains]->(:fs:file WHERE name LIKE '%.go')`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchParser.Parse(query)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuilder_SimpleQuery(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = SelectStar().
			From("nodes").
			Where(Eq("type", String("fs:file"))).
			Build()
	}
}

func BenchmarkBuilder_PatternQuery(b *testing.B) {
	for i := 0; i < b.N; i++ {
		pattern := Pat(N("dir").OfTypeStr("fs:dir").Build()).
			To(EdgeTypeOf("contains").toEdgePattern(), N("file").OfTypeStr("fs:file").Build()).
			Build()
		_ = Select(Var("file")).
			FromPattern(pattern).
			Where(Data.Field("ext").Eq("go")).
			Build()
	}
}

func BenchmarkValidate_PatternQuery(b *testing.B) {
	query := `SELECT file FROM (dir:fs:dir)-[:contains]->(file:fs:file) WHERE file.data.ext = 'go'`
	q, _ := benchParser.Parse(query)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Validate(q)
	}
}
