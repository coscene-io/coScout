package utils

import (
	"regexp"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

func ParseISODuration(duration string) (time.Duration, error) {
	// Regular expression to match ISO 8601 duration format
	re := regexp.MustCompile(`P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)D)?T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?`)
	matches := re.FindStringSubmatch(duration)
	if matches == nil {
		return 0, errors.New("invalid ISO 8601 duration format")
	}

	// Parse components
	years := parseInt(matches[1])
	months := parseInt(matches[2])
	days := parseInt(matches[3])
	hours := parseInt(matches[4])
	minutes := parseInt(matches[5])
	seconds := parseInt(matches[6])

	// Approximate conversion to duration:
	// - 1 year = 365.25 days (to account for leap years)
	// - 1 month = 30 days (rough estimate)

	// Convert years and months to days
	totalDays := years*365 + months*30 + days
	totalHours := hours + totalDays*24
	totalMinutes := minutes + totalHours*60
	totalSeconds := seconds + totalMinutes*60

	// Return as time.Duration (in nanoseconds)
	return time.Duration(totalSeconds) * time.Second, nil
}

// Helper function to parse an integer from a string, defaulting to 0 if empty.
func parseInt(str string) int {
	if str == "" {
		return 0
	}
	value, err := strconv.Atoi(str)
	if err != nil {
		return 0
	}
	return value
}
