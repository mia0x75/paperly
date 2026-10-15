package domain

import "time"

// Valid grade range (grades 1~12, primary through senior high).
const (
	GradeMin = 1
	GradeMax = 12
)

// CurrentSchoolYear returns the school year now belongs to: >= 9/1 is the current year, otherwise
// the previous one.
//
// Single definition shared by two callers: workers compute raw_target when consuming advance_grade;
// daily maintenance fills enrolled_year when enqueuing on 9/1.
func CurrentSchoolYear(now time.Time) int {
	if now.Month() >= time.September {
		return now.Year()
	}
	return now.Year() - 1
}
