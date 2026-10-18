package falkor

// Code Schema Definitions: node labels and relationship types for the code
// knowledge graph produced by the language parsers (see parser.go).
const (
	// Node labels.
	LabelProject  = "Project"
	LabelPackage  = "Package"
	LabelFile     = "File"
	LabelImport   = "Import"
	LabelFunction = "Function"
	LabelStruct   = "Struct"
	LabelField    = "Field"
	LabelVariable = "Variable"

	// Relationship types.
	RelBelongsTo = "BELONGS_TO" // File -> Package
	RelContains  = "CONTAINS"   // Project/Package/File -> child
	RelImports   = "IMPORTS"    // File -> Import
	RelCalls     = "CALLS"      // Function -> Function
	RelHasField  = "HAS_FIELD"  // Struct -> Field
	RelDependsOn = "DEPENDS_ON" // Package -> Package (resolved imports)
)
