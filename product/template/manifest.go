package template

// Manifest is a file as decoded, before normalizing. An empty string is
// a field that was not given; decode refuses an empty one that was.
type Manifest struct {
	Version     int
	MinCubeship string
	Project     string
	Environment string
	Inputs      []Input
	Databases   []Database
	Stores      []Store
	Apps        []App
}

type Input struct {
	Key, Type, Label, Help string
	Required               bool
	Default                any // a string, or a float64 for a number input
	Pattern                string
	Min, Max               *float64
	Options                []string
	Generate               *int
}

type Limits struct {
	CPU    *float64
	Memory *string
}

type Database struct {
	Key, Name, Engine, Version, Username, Database string
	// Expose is nil for internal only, 0 for "pick a port", N for that port.
	Expose *int
	Limits *Limits
}

type Store struct {
	Key, Name, Version string
	Buckets            []string
	Limits             *Limits
}

type Domain struct {
	Host string
	Port *int
}

type Attach struct {
	Database, Store, Bucket, Prefix string
}

type Autoscale struct {
	Min *int
	Max int
	CPU float64
}

type App struct {
	Key, Name, Image, Tag, Repo, Ref, Build, Dockerfile string
	Port                                                *int
	Health                                              string
	Domains                                             []Domain
	Attach                                              []Attach
	Env                                                 map[string]string
	Limits                                              *Limits
	Scale                                               *int
	Spread                                              *bool
	Autoscale                                           *Autoscale
	Volumes                                             []Volume
}

// Volume is a directory inside an app's container that outlives it.
type Volume struct {
	Path string
}
