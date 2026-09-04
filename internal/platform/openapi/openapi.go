// Package openapi is the small subset of OpenAPI 3.1 Cubeship actually
// describes, as Go values.
//
// It is deliberately hand-written rather than generated from annotations:
// each module declares its own operations next to the routes it
// registers, and a test asserts the two match. A generated spec drifts
// silently when someone forgets an annotation; this one fails the build.
package openapi

import "maps"

// Document is a whole OpenAPI document.
type Document struct {
	OpenAPI    string                `json:"openapi"`
	Info       Info                  `json:"info"`
	Servers    []Server              `json:"servers,omitempty"`
	Tags       []Tag                 `json:"tags,omitempty"`
	Paths      map[string]PathItem   `json:"paths"`
	Components Components            `json:"components"`
	Security   []SecurityRequirement `json:"security,omitempty"`
}

type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PathItem maps a lowercase HTTP method to the operation it performs.
type PathItem map[string]*Operation

type Operation struct {
	OperationID string       `json:"operationId"`
	Summary     string       `json:"summary"`
	Description string       `json:"description,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
	Parameters  []Parameter  `json:"parameters,omitempty"`
	RequestBody *RequestBody `json:"requestBody,omitempty"`
	Responses   Responses    `json:"responses"`

	// Security overrides the document-wide requirement. A pointer so an
	// endpoint can declare "no authentication" as an empty list, which is
	// different from inheriting the default.
	Security *[]SecurityRequirement `json:"security,omitempty"`
}

type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Required    bool    `json:"required,omitempty"`
	Description string  `json:"description,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
}

type RequestBody struct {
	Required    bool                 `json:"required,omitempty"`
	Description string               `json:"description,omitempty"`
	Content     map[string]MediaType `json:"content"`
}

type MediaType struct {
	Schema *Schema `json:"schema,omitempty"`
}

type Responses map[string]Response

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

// Schema is the subset of JSON Schema these endpoints need.
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Description          string             `json:"description,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	Example              any                `json:"example,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	Nullable             bool               `json:"nullable,omitempty"`
}

type SecurityRequirement map[string][]string

type Components struct {
	Schemas         map[string]*Schema         `json:"schemas,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes,omitempty"`
}

type SecurityScheme struct {
	Type        string `json:"type"`
	Scheme      string `json:"scheme,omitempty"`
	Description string `json:"description,omitempty"`
}

// Spec is one module's contribution to the document: the paths it serves
// and the schemas those reference.
type Spec struct {
	Tags    []Tag
	Paths   map[string]PathItem
	Schemas map[string]*Schema
}

// Merge folds every spec into one. Two modules declaring the same method
// on the same path is a programming error, and panics rather than
// silently letting one win.
func Merge(specs ...Spec) Spec {
	out := Spec{Paths: map[string]PathItem{}, Schemas: map[string]*Schema{}}
	for _, s := range specs {
		out.Tags = append(out.Tags, s.Tags...)
		for path, item := range s.Paths {
			if out.Paths[path] == nil {
				out.Paths[path] = PathItem{}
			}
			for method, op := range item {
				if _, exists := out.Paths[path][method]; exists {
					panic("openapi: duplicate operation " + method + " " + path)
				}
				out.Paths[path][method] = op
			}
		}
		maps.Copy(out.Schemas, s.Schemas)
	}
	return out
}

// --- constructors, so declarations read as data rather than punctuation ---

// Ref points at a component schema by name.
func Ref(name string) *Schema {
	return &Schema{Ref: "#/components/schemas/" + name}
}

func String(description string) *Schema {
	return &Schema{Type: "string", Description: description}
}

func Integer(description string) *Schema {
	return &Schema{Type: "integer", Format: "int64", Description: description}
}

func Bool(description string) *Schema {
	return &Schema{Type: "boolean", Description: description}
}

func Array(items *Schema) *Schema {
	return &Schema{Type: "array", Items: items}
}

// StringMap is an object whose values are all strings — an env var map.
func StringMap(description string) *Schema {
	return &Schema{
		Type:                 "object",
		Description:          description,
		AdditionalProperties: &Schema{Type: "string"},
	}
}

func Object(properties map[string]*Schema, required ...string) *Schema {
	return &Schema{Type: "object", Properties: properties, Required: required}
}

// PathParam declares a required path parameter.
func PathParam(name, description string) Parameter {
	return Parameter{Name: name, In: "path", Required: true, Description: description, Schema: &Schema{Type: "string"}}
}

// QueryParam declares an optional query parameter.
func QueryParam(name, description string) Parameter {
	return Parameter{Name: name, In: "query", Description: description, Schema: &Schema{Type: "string"}}
}

// JSON wraps a schema as an application/json body.
func JSON(schema *Schema) map[string]MediaType {
	return map[string]MediaType{"application/json": {Schema: schema}}
}

// Body is a required JSON request body.
func Body(schema *Schema) *RequestBody {
	return &RequestBody{Required: true, Content: JSON(schema)}
}

// JSONResponse is a response carrying a JSON body.
func JSONResponse(description string, schema *Schema) Response {
	return Response{Description: description, Content: JSON(schema)}
}

// Empty is a response with no body.
func Empty(description string) Response {
	return Response{Description: description}
}

// Public marks an operation as needing no authentication.
func Public() *[]SecurityRequirement {
	return &[]SecurityRequirement{}
}

// Requires names a security scheme other than the document default.
func Requires(scheme string) *[]SecurityRequirement {
	return &[]SecurityRequirement{{scheme: {}}}
}

// TextResponse is a response whose body is a plain-text message —
// what http.Error writes, and therefore what every failure on this API
// looks like.
func TextResponse(description string) Response {
	return Response{
		Description: description,
		Content:     map[string]MediaType{"text/plain": {Schema: &Schema{Type: "string"}}},
	}
}

// The failure responses nearly every authenticated endpoint can return.
// Spelling them out per endpoint would be noise; leaving them out would
// make the document misleading.
var (
	Unauthorized = TextResponse("The Authorization header is missing, malformed, or carries an unknown API key.")

	// Forbidden is only ever returned to someone who already belongs to
	// the organization — see NotFound.
	Forbidden = TextResponse("You belong to this organization but lack the role this action requires.")

	// NotFound deliberately covers two cases: the resource does not
	// exist, and you are not a member of the organization that owns it.
	// They are indistinguishable on purpose, so a valid API key cannot be
	// used to enumerate other tenants.
	NotFound = TextResponse("The resource does not exist, or belongs to an organization you are not a member of.")

	BadRequest = TextResponse("A required field is missing, or a slug is not lowercase letters, digits and dashes.")
)
