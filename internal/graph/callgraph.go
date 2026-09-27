package graph

type Function struct {
	ID       string
	Name     string
	Package  string
	Receiver *Param
	Params   []Param
	Results  []Param
	Exported bool
	Pos      string
}

type Param struct {
	Name string
	Type string
}

type FunctionCall struct {
	Caller         string
	Callee         string
	CallerPosition string
	CallKind       CallKind
}

type CallKind string

const (
	StaticCall    CallKind = "static"
	InterfaceCall CallKind = "interface"
	DynamicCall   CallKind = "dynamic"
)
