// Package linter handles logic around linting schemas and returning results.
package linter

import (
	"fmt"

	"github.com/skeema/mybase"
	"github.com/skeema/skeema/fs"
	"github.com/skeema/skeema/workspace"
	//	"github.com/skeema/tengo"
)

// Annotation is an error, warning, or notice from linting a single SQL
// statement.
type Annotation struct {
	Statement  *fs.Statement
	LineOffset int
	Summary    string
	Message    string
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

// AddOptions adds linting-related options to the supplied mybase.Command.
func AddOptions(cmd *mybase.Command) {
	cmd.AddOption(mybase.StringOption("lint-warning", 0, "bad-charset,bad-engine", "Linter problems to treat as warnings; see full docs for usage"))
	cmd.AddOption(mybase.StringOption("lint-error", 0, "no-pk", "Linter problems to treat as errors; see full docs for usage"))
	cmd.AddOption(mybase.StringOption("lint-allowed-charset", 0, "latin1,utf8mb4", "Whitelist of acceptable character sets"))
	cmd.AddOption(mybase.StringOption("lint-allowed-engine", 0, "innodb", "Whitelist of acceptable storage engines"))
}

// LintDir lints dir and its subdirs, returning a cumulative result.
func LintDir(dir *fs.Dir, opts workspace.Options) *Result {
	result := &Result{}

	ignoreTable, err := dir.Config.GetRegexp("ignore-table")
	if err != nil {
		return BadConfigResult(err)
	}
	ignoreSchema, err := dir.Config.GetRegexp("ignore-schema")
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

		schema, statementErrors, err := workspace.ExecLogicalSchema(logicalSchema, opts)
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
				Message:   stmtErr.Error(),
			})
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

// ConfigError represents a configuration problem encountered at runtime.
type ConfigError string

// Error satisfies the builtin error interface.
func (ce ConfigError) Error() string {
	return string(ce)
}
