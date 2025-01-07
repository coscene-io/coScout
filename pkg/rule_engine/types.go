package rule_engine

import "github.com/google/cel-go/cel"

// ErrorSection represents the section where validation error occurred
type ErrorSection int

const (
	ConditionSection ErrorSection = iota + 1
	ActionSection
)

// ValidationErrorLocation represents where the validation error occurred
type ValidationErrorLocation struct {
	RuleIndex int          `json:"ruleIndex"`
	Section   ErrorSection `json:"section"`
	ItemIndex int          `json:"itemIndex,omitempty"`
}

// ValidationErrorUnexpectedVersion represents version validation error
type ValidationErrorUnexpectedVersion struct {
	AllowedVersions []string `json:"allowedVersions"`
}

// ValidationError represents a validation error
type ValidationError struct {
	Location          *ValidationErrorLocation          `json:"location,omitempty"`
	UnexpectedVersion *ValidationErrorUnexpectedVersion `json:"unexpectedVersion,omitempty"`
	SyntaxError       *struct{}                         `json:"syntaxError,omitempty"`
	EmptySection      *struct{}                         `json:"emptySection,omitempty"`
}

// ValidationResult represents the result of validation
type ValidationResult struct {
	Success bool              `json:"success"`
	Errors  []ValidationError `json:"errors"`
}

func CompareValidationResult(result1, result2 ValidationResult) bool {
	if result1.Success != result2.Success {
		return false
	}
	if len(result1.Errors) != len(result2.Errors) {
		return false
	}

	// Compare error counts
	for idx, _ := range result1.Errors {
		if !result1.Errors[idx].Equal(result2.Errors[idx]) {
			return false
		}
	}

	return true
}

// Add this helper method to make ValidationError comparable
func (e ValidationError) Equal(other ValidationError) bool {
	// Compare Location
	if (e.Location == nil) != (other.Location == nil) {
		return false
	}
	if e.Location != nil && !e.Location.Equal(*other.Location) {
		return false
	}

	// Compare UnexpectedVersion
	if (e.UnexpectedVersion == nil) != (other.UnexpectedVersion == nil) {
		return false
	}
	if e.UnexpectedVersion != nil {
		if !sliceEqual(e.UnexpectedVersion.AllowedVersions, other.UnexpectedVersion.AllowedVersions) {
			return false
		}
	}

	// Compare presence of SyntaxError
	if (e.SyntaxError == nil) != (other.SyntaxError == nil) {
		return false
	}

	// Compare presence of EmptySection
	if (e.EmptySection == nil) != (other.EmptySection == nil) {
		return false
	}

	return true
}

// Add this helper method for ValidationErrorLocation
func (l ValidationErrorLocation) Equal(other ValidationErrorLocation) bool {
	return l.RuleIndex == other.RuleIndex &&
		l.Section == other.Section &&
		l.ItemIndex == other.ItemIndex
}

// Helper function to compare string slices
func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func NewEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("msg", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("scope", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("topic", cel.StringType),
		cel.Variable("ts", cel.DoubleType),
	)
}
