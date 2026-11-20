package main

import (
	_ "embed"
	"encoding/json"
)

// The Go executable and the JavaScript package use the same version matrix.
//
//go:embed src/matrix.json
var matrix json.RawMessage

var supportedMajors = func() map[string]bool {
	var data struct {
		Rows []struct {
			Major json.Number `json:"major"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(matrix, &data); err != nil {
		panic("invalid embedded PostgreSQL version matrix")
	}
	majors := make(map[string]bool, len(data.Rows))
	for _, row := range data.Rows {
		majors[row.Major.String()] = true
	}
	return majors
}()

type policies struct {
	JSON  string `json:"json"`
	Enum  string `json:"enum"`
	Array string `json:"array"`
}

type profile struct {
	EngineVersion string   `json:"engineVersion"`
	Policies      policies `json:"policies"`
	Extensions    []struct {
		Name     string `json:"name"`
		State    string `json:"state"`
		Required bool   `json:"required"`
	} `json:"extensions"`
	Tenancy struct {
		Enforcement string `json:"enforcement"`
		RLS         *struct {
			SessionVariable string `json:"sessionVariable"`
			Force           bool   `json:"force"`
		} `json:"rls"`
	} `json:"tenancy"`
}

type domainType struct {
	Name      string      `json:"name"`
	Length    *int        `json:"length"`
	Precision *int        `json:"precision"`
	Scale     *int        `json:"scale"`
	Element   *domainType `json:"element"`
}

type literal struct {
	Kind  string          `json:"kind"`
	Value json.RawMessage `json:"value"`
}

type defaultValue struct {
	Kind  string   `json:"kind"`
	Value *literal `json:"value"`
	Ref   string   `json:"ref"`
}

type column struct {
	Name       string        `json:"name"`
	Type       *string       `json:"type"`
	SourceType *domainType   `json:"sourceType"`
	Nullable   bool          `json:"nullable"`
	Origin     string        `json:"origin"`
	Generated  string        `json:"generated"`
	Default    *defaultValue `json:"default"`
}

type predicate struct {
	Column string   `json:"column"`
	Op     string   `json:"op"`
	Value  *literal `json:"value"`
}

type index struct {
	Name    *string     `json:"name"`
	Columns []string    `json:"columns"`
	Unique  bool        `json:"unique"`
	Where   []predicate `json:"where"`
}

type table struct {
	Table       string              `json:"table"`
	Columns     []column            `json:"columns"`
	PrimaryKey  []string            `json:"primaryKey"`
	EnumMembers map[string][]string `json:"enumMembers"`
	Indexes     []index             `json:"indexes"`
	Checks      []struct {
		Name       string      `json:"name"`
		Predicates []predicate `json:"predicates"`
	} `json:"checks"`
	ForeignKeys []struct {
		Column          string `json:"column"`
		ReferencesTable string `json:"referencesTable"`
		OnDelete        string `json:"onDelete"`
	} `json:"foreignKeys"`
}

type joinReference struct {
	Table  string `json:"table"`
	Column string `json:"column"`
}

type joinTable struct {
	Table          string           `json:"table"`
	Columns        []column         `json:"columns"`
	UniquePair     bool             `json:"uniquePair"`
	OnOwnerDelete  string           `json:"onOwnerDelete"`
	OnTargetDelete string           `json:"onTargetDelete"`
	References     []*joinReference `json:"references"`
}

type projection struct {
	Tables []table     `json:"tables"`
	Joins  []joinTable `json:"joins"`
}

type refusal struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// Rendering is pure. Keep the first refusal as fragments are assembled and
// return it instead of the result, so partial SQL never reaches the host.
type renderer struct {
	err *refusal
}

func (r *renderer) refuse(kind, detail string) string {
	if r.err == nil {
		r.err = &refusal{Kind: kind, Detail: detail}
	}
	return ""
}

func readInput[T any](r *renderer, data json.RawMessage) T {
	var input T
	if json.Unmarshal(data, &input) != nil {
		r.refuse("mapping", "input-invalid")
	}
	return input
}

func dispatch(operation string, data json.RawMessage) (any, *refusal) {
	var r renderer
	var result any
	switch operation {
	case "storage.matrix":
		result = matrix
	case "storage.type":
		input := readInput[struct {
			Type     *domainType `json:"type"`
			Policies policies    `json:"policies"`
		}](&r, data)
		result = r.mapType(input.Type, input.Policies, 0)
	case "storage.ddl":
		input := readInput[struct {
			Profile    profile    `json:"profile"`
			Projection projection `json:"projection"`
		}](&r, data)
		result = r.renderDDL(input.Profile, input.Projection)
	case "storage.table":
		input := readInput[struct {
			Profile profile `json:"profile"`
			Table   table   `json:"table"`
		}](&r, data)
		result = r.createTable(input.Profile, input.Table)
	case "storage.join":
		input := readInput[struct {
			Join joinTable `json:"join"`
		}](&r, data)
		result = r.createJoin(input.Join)
	case "storage.default":
		input := readInput[struct {
			Table string        `json:"table"`
			Value *defaultValue `json:"value"`
		}](&r, data)
		result = r.renderDefault(input.Value, input.Table)
	case "storage.sequence":
		input := readInput[struct {
			Table  string `json:"table"`
			Column string `json:"column"`
		}](&r, data)
		result = r.sequenceDefault(input.Table, input.Column)
	case "storage.index-name":
		input := readInput[struct {
			Table string `json:"table"`
			Index index  `json:"index"`
		}](&r, data)
		result = r.indexName(input.Table, input.Index)
	default:
		r.refuse("mapping", "operation-unsupported")
	}
	return result, r.err
}
