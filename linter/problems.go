package linter

import (
	"fmt"
	"strings"

	"github.com/skeema/skeema/fs"
	"github.com/skeema/tengo"
)

// detector functions operate on a per-schema level of granularity, even though
// the current detectors all just operate on individual tables. This results in
// a bit more boilerplate and inefficiency, but it facilitates future support
// for non-table object types, and possibly cross-table problem detection.
type detector func(*tengo.Schema, *fs.LogicalSchema, Options) []*Annotation

var problems map[string]detector

func init() {
	problems = map[string]detector{
		"no-pk":       noPKDetector,
		"bad-charset": badCharsetDetector,
		"bad-engine":  badEngineDetector,
	}
}

func noPKDetector(schema *tengo.Schema, logicalSchema *fs.LogicalSchema, _ Options) []*Annotation {
	results := make([]*Annotation, 0)
	for _, table := range schema.Tables {
		if table.PrimaryKey == nil {
			results = append(results, &Annotation{
				Statement: logicalSchema.CreateTables[table.Name],
				Summary:   "No primary key",
				Message:   fmt.Sprintf("Table %s does not define a PRIMARY KEY", table.Name),
			})
		}
	}
	return results
}

func badCharsetDetector(schema *tengo.Schema, logicalSchema *fs.LogicalSchema, opts Options) []*Annotation {
	results := make([]*Annotation, 0)
	for _, table := range schema.Tables {
		if !isAllowed(table.CharSet, opts.AllowedCharSets) {
			results = append(results, &Annotation{
				Statement: logicalSchema.CreateTables[table.Name],
				Summary:   "Character set not permitted",
				Message:   fmt.Sprintf("Table %s is using character set %s, which is not in lint-allowed-charset", table.Name, table.CharSet),
			})
		}
	}
	return results
}

func badEngineDetector(schema *tengo.Schema, logicalSchema *fs.LogicalSchema, opts Options) []*Annotation {
	results := make([]*Annotation, 0)
	for _, table := range schema.Tables {
		if !isAllowed(table.Engine, opts.AllowedEngines) {
			results = append(results, &Annotation{
				Statement: logicalSchema.CreateTables[table.Name],
				Summary:   "Storage engine not permitted",
				Message:   fmt.Sprintf("Table %s is using storage engine %s, which is not in lint-allowed-engine", table.Name, table.Engine),
			})
		}
	}

	return results
}

func problemExists(name string) bool {
	_, ok := problems[strings.ToLower(name)]
	return ok
}

func allProblemNames() []string {
	result := make([]string, 0, len(problems))
	for name := range problems {
		result = append(result, name)
	}
	return result
}

// isAllowed performs a case-insensitive search for value in allowed, returning
// true if found.
func isAllowed(value string, allowed []string) bool {
	value = strings.ToLower(value)
	for _, allowedValue := range allowed {
		if value == strings.ToLower(allowedValue) {
			return true
		}
	}
	return false
}
