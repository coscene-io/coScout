package model

type RecordCache struct {
	Uploaded    bool   `json:"uploaded" yaml:"uploaded"`
	Skipped     bool   `json:"skipped" yaml:"skipped"`
	EventCode   string `json:"event_code" yaml:"event_code"`
	ProjectName string `json:"project_name" yaml:"project_name"`

	// Timestamp milliseconds
	Timestamp int64                  `json:"timestamp" yaml:"timestamp"`
	Labels    []string               `json:"labels" yaml:"labels"`
	Record    map[string]interface{} `json:"record" yaml:"record"`
	Moments   []Moment               `json:"moments" yaml:"moments"`

	UploadTask    map[string]interface{} `json:"task" yaml:"task"`
	DiagnosisTask map[string]interface{} `json:"diagnosis_task" yaml:"diagnosis_task"`

	OriginalFiles     []string `json:"files" yaml:"files"`
	UploadedFilePaths []string `json:"uploaded_filepaths" yaml:"uploaded_filepaths"`
}

type Moment struct {
	Name        string `json:"name" yaml:"name"`
	IsNew       bool   `json:"is_new" yaml:"is_new"`
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`
	// Timestamp seconds
	Timestamp float64 `json:"timestamp" yaml:"timestamp"`
	// Duration seconds
	Duration float64           `json:"duration" yaml:"duration"`
	Metadata map[string]string `json:"metadata" yaml:"metadata"`
	Task
}

type Task struct {
	Name        string `json:"name" yaml:"name"`
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`
	RecordName  string `json:"record_name" yaml:"record_name"`
	Assignee    string `json:"assignee" yaml:"assignee"`
	SyncTask    bool   `json:"sync_task" yaml:"sync_task"`
}
