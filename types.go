package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	namePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	integerPattern = regexp.MustCompile(`^-?[0-9]+$`)
	decimalPattern = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
)

func (r *renderer) name(value, detail string) string {
	if !namePattern.MatchString(value) {
		return r.refuse("mapping", detail)
	}
	return value
}

func (r *renderer) quote(value string) string {
	return `"` + r.name(value, "identifier") + `"`
}

func (r *renderer) quoteColumns(columns []string) string {
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = r.quote(column)
	}
	return strings.Join(quoted, ", ")
}

func quoteText(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func (r *renderer) renderLiteral(value *literal) any {
	if value == nil {
		return r.refuse("mapping", "literal")
	}
	switch value.Kind {
	case "boolean":
		var parsed *bool
		if json.Unmarshal(value.Value, &parsed) != nil || parsed == nil {
			return r.refuse("mapping", "literal")
		}
		if *parsed {
			return "TRUE"
		}
		return "FALSE"
	case "integer":
		return r.renderNumber(value.Value, integerPattern)
	case "decimal":
		return r.renderNumber(value.Value, decimalPattern)
	case "text":
		var text *string
		if json.Unmarshal(value.Value, &text) == nil && text != nil {
			return quoteText(*text)
		}
	}
	return r.refuse("mapping", "literal")
}

func (r *renderer) renderNumber(raw json.RawMessage, pattern *regexp.Regexp) any {
	// The host sends decimal text to preserve exact precision. Preserve the
	// legacy JSON number response for callers that send a JSON number.
	var text *string
	if json.Unmarshal(raw, &text) == nil && text != nil {
		if pattern.MatchString(*text) {
			return *text
		}
		return r.refuse("mapping", "literal")
	}

	var number *float64
	if json.Unmarshal(raw, &number) == nil && number != nil {
		if encoded, err := json.Marshal(*number); err == nil && pattern.Match(encoded) {
			return json.Number(encoded)
		}
	}
	return r.refuse("mapping", "literal")
}

func (r *renderer) mapType(t *domainType, p policies, depth int) string {
	if depth > 32 {
		return r.refuse("mapping", "type-depth")
	}
	if t == nil {
		return r.refuse("mapping", "domain-type")
	}
	switch t.Name {
	case "boolean", "text", "uuid", "date":
		return t.Name
	case "integer":
		return "bigint"
	case "timestamp":
		return "timestamptz"
	case "binary":
		return "bytea"
	case "decimal":
		if t.Precision == nil || t.Scale == nil {
			return r.refuse("mapping", "decimal-type")
		}
		return fmt.Sprintf("numeric(%d,%d)", *t.Precision, *t.Scale)
	case "string":
		if t.Length == nil || *t.Length <= 0 {
			return r.refuse("mapping", "string-type")
		}
		return fmt.Sprintf("varchar(%d)", *t.Length)
	case "json":
		if p.JSON != "json" && p.JSON != "jsonb" {
			return r.refuse("mapping", "json-policy")
		}
		return p.JSON
	case "enum":
		if p.Enum == "native_enum" {
			return r.refuse("render", "native-enum")
		}
		if p.Enum != "check" {
			return r.refuse("mapping", "enum-policy")
		}
		return "varchar(64)"
	case "array":
		switch p.Array {
		case "native":
			return r.mapType(t.Element, p, depth+1) + "[]"
		case "json":
			return "jsonb"
		case "unsupported":
			return r.refuse("render", "array-unsupported")
		default:
			return r.refuse("mapping", "array-policy")
		}
	default:
		return r.refuse("mapping", "domain-type")
	}
}

func (r *renderer) storageType(value *string) string {
	if value == nil || len(*value) > 512 || strings.ContainsAny(*value, ";\n\r\x00") {
		return r.refuse("mapping", "storage-type")
	}
	return *value
}

func (r *renderer) columnType(p policies, c column) string {
	if c.SourceType != nil {
		return r.mapType(c.SourceType, p, 0)
	}
	return r.storageType(c.Type)
}

func (r *renderer) sequenceName(table, column string) string {
	return r.name("seq_"+r.name(table, "identifier")+"_"+r.name(column, "identifier"), "sequence-name")
}

func (r *renderer) sequenceDefault(table, column string) string {
	return "nextval('" + r.sequenceName(table, column) + "')"
}

func (r *renderer) renderDefault(value *defaultValue, table string) any {
	if value == nil {
		return r.refuse("mapping", "default-kind")
	}
	switch value.Kind {
	case "literal":
		return r.renderLiteral(value.Value)
	case "now":
		return "now()"
	case "uuid_generate":
		return "gen_random_uuid()"
	case "sequence":
		return r.sequenceDefault(table, value.Ref)
	default:
		return r.refuse("mapping", "default-kind")
	}
}

func (r *renderer) indexName(table string, index index) string {
	name := "idx_" + r.name(table, "identifier") + "_"
	columns := make([]string, len(index.Columns))
	for i, column := range index.Columns {
		columns[i] = r.name(column, "identifier")
	}
	name += strings.Join(columns, "_")
	if index.Unique {
		name += "_uq"
	}
	return r.name(name, "index-name")
}
