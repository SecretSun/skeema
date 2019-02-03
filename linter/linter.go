// Package linter handles logic around linting schemas and returning results.
package linter

import (
	"fmt"

	"github.com/skeema/skeema/fs"
	"github.com/skeema/skeema/workspace"
)

// Annotation is an error, warning, or notice from linting a single SQL
// statement.
type Annotation struct {
	Statement  *fs.Statement
	LineOffset int
	Summary    string
	Message    string
}

// MessageWithLocation prepends statement location information to a.Message,
// if location information is available. Otherwise, it appends the full SQL
// statement that the message refers to.
func (a *Annotation) MessageWithLocation() string {
	loc := a.Statement.Location()
	if loc == "" {
		return fmt.Sprintf("%s [Full SQL: %s]", a.Message, a.Statement.Text)
	}
	return fmt.Sprintf("%s: %s", loc, a.Message)
}

// Result is a combined set of linter annotations and/or Golang errors found
// when linting a directory and its subdirs.
type Result struct {
	Errors        []*Annotation // "Errors" in the linting sense, not in the Golang sense
	Warnings      []*Annotation
	FormatNotices []*Annotation
	DebugLogs     []string
	Exceptions    []error
}

// Merge combines other into r's value in-place.
func (r *Result) Merge(other *Result) {
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
	r.FormatNotices = append(r.FormatNotices, other.FormatNotices...)
	r.DebugLogs = append(r.DebugLogs, other.DebugLogs...)
	r.Exceptions = append(r.Exceptions, other.Exceptions...)
}

// BadConfigResult returns a *Result containing a single ConfigError in the
// Exceptions field. The supplied err will be converted to a ConfigError if it
// is not already one.
func BadConfigResult(err error) *Result {
	if _, ok := err.(ConfigError); !ok {
		err = ConfigError(err.Error())
	}
	return &Result{
		Exceptions: []error{err},
	}
}

// LintDir lints dir and its subdirs, returning a cumulative result.
func LintDir(dir *fs.Dir, wsOpts workspace.Options) *Result {
	result := &Result{}

	ignoreTable, err := dir.Config.GetRegexp("ignore-table")
	if err != nil {
		return BadConfigResult(err)
	}
	ignoreSchema, err := dir.Config.GetRegexp("ignore-schema")
	if err != nil {
		return BadConfigResult(err)
	}
	opts, err := OptionsForDir(dir)
	if err != nil {
		return BadConfigResult(err)
	}

	for _, logicalSchema := range dir.LogicalSchemas {
		// ignore-schema is handled relatively simplistically here: skip dir entirely
		// if any literal schema name matches the pattern, but don't bother
		// interpretting schema=`shellout` or schema=*, which require an instance.
		if ignoreSchema != nil {
			var foundIgnoredName bool
			for _, schemaName := range dir.Config.GetSlice("schema", ',', true) {
				if ignoreSchema.MatchString(schemaName) {
					foundIgnoredName = true
				}
			}
			if foundIgnoredName {
				result.DebugLogs = append(result.DebugLogs, fmt.Sprintf("Skipping schema in %s because ignore-schema='%s'", dir.RelPath(), ignoreSchema))
				return result
			}
		}

		schema, statementErrors, err := workspace.ExecLogicalSchema(logicalSchema, wsOpts)
		if err != nil {
			result.Exceptions = append(result.Exceptions, fmt.Errorf("Skipping schema in %s due to error: %s", dir.RelPath(), err))
			continue
		}
		for _, stmtErr := range statementErrors {
			if ignoreTable != nil && ignoreTable.MatchString(stmtErr.TableName) {
				result.DebugLogs = append(result.DebugLogs, fmt.Sprintf("Skipping table %s because ignore-table='%s'", stmtErr.TableName, ignoreTable))
				continue
			}
			result.Errors = append(result.Errors, &Annotation{
				Statement: stmtErr.Statement,
				Summary:   "SQL statement returned an error",
				Message:   stmtErr.Err.Error(),
			})
		}

		for problemName, severity := range opts.ProblemSeverity {
			annotations := problems[problemName](schema, logicalSchema, opts)
			if severity == SeverityWarning {
				result.Warnings = append(result.Warnings, annotations...)
			} else {
				result.Errors = append(result.Errors, annotations...)
			}
		}

		for _, table := range schema.Tables {
			if ignoreTable != nil && ignoreTable.MatchString(table.Name) {
				result.DebugLogs = append(result.DebugLogs, fmt.Sprintf("Skipping table %s because ignore-table='%s'", table.Name, ignoreTable))
				continue
			}
			body, suffix := logicalSchema.CreateTables[table.Name].SplitTextBody()
			if table.CreateStatement != body {
				result.FormatNotices = append(result.FormatNotices, &Annotation{
					Statement: logicalSchema.CreateTables[table.Name],
					Summary:   "SQL statement should be reformatted",
					Message:   fmt.Sprintf("%s%s", table.CreateStatement, suffix),
				})
			}
		}
	}
	return result
}
