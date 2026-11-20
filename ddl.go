package main

import (
	"fmt"
	"regexp"
	"strings"
)

var sessionVariablePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z0-9_]+)*$`)

type statement struct {
	ID        int    `json:"id"`
	Statement string `json:"statement"`
}

type ddlDocument struct {
	EngineVersion string      `json:"engineVersion"`
	Statements    []statement `json:"statements"`
}

func (r *renderer) renderPredicates(rows []predicate) string {
	rendered := make([]string, len(rows))
	for i, row := range rows {
		column := r.quote(row.Column)
		switch row.Op {
		case "is-null":
			rendered[i] = column + " IS NULL"
		case "is-not-null":
			rendered[i] = column + " IS NOT NULL"
		case "eq", "ne":
			if row.Value == nil {
				rendered[i] = column + " IS NOT NULL"
				continue
			}
			operator := " = "
			if row.Op == "ne" {
				operator = " <> "
			}
			rendered[i] = column + operator + fmt.Sprint(r.renderLiteral(row.Value))
		default:
			return r.refuse("mapping", "predicate")
		}
	}
	return strings.Join(rendered, " AND ")
}

func (r *renderer) createTable(p profile, t table) string {
	if len(t.EnumMembers) != 0 && p.Policies.Enum == "native_enum" {
		return r.refuse("render", "native-enum")
	}

	lines := make([]string, 0, len(t.Columns)+len(t.Checks)+len(t.EnumMembers)+1)
	for _, c := range t.Columns {
		line := r.quote(c.Name) + " " + r.columnType(p.Policies, c)
		switch c.Generated {
		case "identity":
			line += " GENERATED ALWAYS AS IDENTITY"
		case "computed":
			return r.refuse("render", "computed-column")
		case "sequence":
			line += " DEFAULT " + r.sequenceDefault(t.Table, c.Name)
		default:
			if c.Default != nil {
				line += " DEFAULT " + fmt.Sprint(r.renderDefault(c.Default, t.Table))
			}
		}
		if !c.Nullable {
			line += " NOT NULL"
		}
		lines = append(lines, line)
	}

	lines = append(lines, fmt.Sprintf(`CONSTRAINT "pk_%s" PRIMARY KEY (%s)`, r.name(t.Table, "identifier"), r.quoteColumns(t.PrimaryKey)))
	for _, check := range t.Checks {
		lines = append(lines, fmt.Sprintf("CONSTRAINT %s CHECK (%s)", r.quote(check.Name), r.renderPredicates(check.Predicates)))
	}
	// Enum constraints follow the column order, regardless of the JSON object
	// order or Go's map iteration order.
	for _, c := range t.Columns {
		members := t.EnumMembers[c.Name]
		if members == nil {
			continue
		}
		constraint := r.name("chk_"+t.Table+"_"+c.Name, "check-name")
		values := make([]string, len(members))
		for i, member := range members {
			values[i] = quoteText(member)
		}
		lines = append(lines, fmt.Sprintf("CONSTRAINT %s CHECK (%s IN (%s))", r.quote(constraint), r.quote(c.Name), strings.Join(values, ", ")))
	}
	return fmt.Sprintf("CREATE TABLE %s (%s);", r.quote(t.Table), strings.Join(lines, ", "))
}

func (r *renderer) createJoin(join joinTable) string {
	lines := make([]string, 0, len(join.Columns)+1)
	columns := make([]string, len(join.Columns))
	for i, c := range join.Columns {
		columns[i] = r.quote(c.Name)
		lines = append(lines, columns[i]+" "+r.columnType(policies{}, c)+" NOT NULL")
	}
	if join.UniquePair {
		lines = append(lines, fmt.Sprintf(`CONSTRAINT "pk_%s" PRIMARY KEY (%s)`, r.name(join.Table, "identifier"), strings.Join(columns, ", ")))
	}
	return fmt.Sprintf("CREATE TABLE %s (%s);", r.quote(join.Table), strings.Join(lines, ", "))
}

func (r *renderer) foreignKey(table, column, target, key, action string) string {
	if key == "" {
		return r.refuse("mapping", "referenced-key-absent")
	}
	switch action {
	case "CASCADE", "RESTRICT", "SET NULL", "NO ACTION", "SET DEFAULT":
	default:
		return r.refuse("mapping", "delete-action")
	}
	constraint := r.name("fk_"+table+"_"+column, "foreign-key-name")
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE %s;",
		r.quote(table), r.quote(constraint), r.quote(column), r.quote(target), r.quote(key), action)
}

func (r *renderer) createIndex(table string, index index) string {
	var name string
	if index.Name == nil {
		name = r.indexName(table, index)
	} else {
		name = *index.Name
	}
	unique := ""
	if index.Unique {
		unique = "UNIQUE "
	}
	statement := fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", unique, r.quote(name), r.quote(table), r.quoteColumns(index.Columns))
	if index.Where != nil {
		statement += " WHERE " + r.renderPredicates(index.Where)
	}
	return statement + ";"
}

func (r *renderer) renderDDL(p profile, projection projection) ddlDocument {
	out := ddlDocument{EngineVersion: p.EngineVersion, Statements: []statement{}}
	major, _, _ := strings.Cut(p.EngineVersion, ".")
	if !supportedMajors[major] {
		r.refuse("mapping", "major-unpublished")
		return out
	}

	add := func(sql string) {
		out.Statements = append(out.Statements, statement{ID: len(out.Statements) + 1, Statement: sql})
	}
	for _, extension := range p.Extensions {
		if extension.Required || extension.State == "required" {
			add("CREATE EXTENSION IF NOT EXISTS " + r.quote(extension.Name) + ";")
		}
	}

	var sequenceOwners []string
	for _, table := range projection.Tables {
		for _, c := range table.Columns {
			if c.Generated == "sequence" {
				sequence := r.quote(r.sequenceName(table.Table, c.Name))
				add("CREATE SEQUENCE " + sequence + ";")
				sequenceOwners = append(sequenceOwners, "ALTER SEQUENCE "+sequence+" OWNED BY "+r.quote(table.Table)+"."+r.quote(c.Name)+";")
			}
		}
	}

	keys := make(map[string]string, len(projection.Tables))
	for _, table := range projection.Tables {
		add(r.createTable(p, table))
		if len(table.PrimaryKey) > 0 {
			keys[table.Table] = table.PrimaryKey[0]
		}
	}
	for _, join := range projection.Joins {
		add(r.createJoin(join))
	}

	for _, table := range projection.Tables {
		for _, key := range table.ForeignKeys {
			add(r.foreignKey(table.Table, key.Column, key.ReferencesTable, keys[key.ReferencesTable], key.OnDelete))
		}
	}
	for _, join := range projection.Joins {
		for i, action := range []string{join.OnOwnerDelete, join.OnTargetDelete} {
			if i >= len(join.References) || join.References[i] == nil || i >= len(join.Columns) {
				r.refuse("mapping", "join-reference-unresolved")
				return out
			}
			ref := join.References[i]
			add(r.foreignKey(join.Table, join.Columns[i].Name, ref.Table, ref.Column, action))
		}
	}
	for _, table := range projection.Tables {
		for _, index := range table.Indexes {
			add(r.createIndex(table.Table, index))
		}
	}
	for _, owner := range sequenceOwners {
		add(owner)
	}

	if p.Tenancy.Enforcement == "rls" {
		for _, sql := range r.renderRLS(p, projection.Tables) {
			add(sql)
		}
	}
	return out
}

func (r *renderer) renderRLS(p profile, tables []table) []string {
	rls := p.Tenancy.RLS
	if rls == nil {
		r.refuse("mapping", "rls-policy-missing")
		return nil
	}
	variable := rls.SessionVariable
	if len(variable) < 3 || len(variable) > 63 || !sessionVariablePattern.MatchString(variable) {
		r.refuse("mapping", "rls-session-variable")
		return nil
	}

	var statements []string
	for _, table := range tables {
		for _, tenant := range table.Columns {
			if tenant.Origin != "tenant_key" {
				continue
			}
			name := r.quote(table.Table)
			statements = append(statements, "ALTER TABLE "+name+" ENABLE ROW LEVEL SECURITY;")
			if rls.Force {
				statements = append(statements, "ALTER TABLE "+name+" FORCE ROW LEVEL SECURITY;")
			}
			policy := r.name("pol_"+table.Table+"_tenant", "policy-name")
			statements = append(statements, fmt.Sprintf("CREATE POLICY %s ON %s USING (%s = current_setting('%s')::%s);",
				r.quote(policy), name, r.quote(tenant.Name), variable, r.storageType(tenant.Type)))
			break
		}
	}
	return statements
}
