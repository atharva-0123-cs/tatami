package falkor

// Graph Schema Definitions
const (
	LabelWindow    = "Window"
	LabelNode      = "Node"
	LabelPane      = "Pane"
	LabelDirectory = "Directory"
	LabelProcess   = "Process"
	LabelAgent     = "Agent"
	LabelBug       = "Bug"
	LabelFinding   = "Finding"
	LabelTask      = "Task"
	LabelCommand   = "Command"
	LabelOutput    = "Output"
	LabelSession   = "Session"

	RelRoot         = "ROOT"
	RelChild        = "CHILD"
	RelContent      = "CONTAINS"
	RelWorkingIn    = "WORKING_IN"
	RelChildProcess = "CHILD_PROCESS"
	RelExecutes     = "EXECUTES"
	RelFound        = "FOUND"
	RelAffects      = "AFFECTS"
	RelExecuted     = "EXECUTED"
	RelAffected     = "AFFECTED"
	RelAssigned     = "ASSIGNED_TO"
	RelBlocks       = "BLOCKS"
	RelReports      = "REPORTS"
	RelProduces     = "PRODUCES"
)
