package surveydoc

// A date element's answer is YYYY-MM-DD, or YYYY-MM-DDTHH:mm when the element includes the time of day (packages/types/surveys/date-answer.ts).

// HasTimeOfDay reports whether a date answer carries a time of day.
func HasTimeOfDay(answer string) bool { return len(answer) > 10 && answer[10] == 'T' }

// AnswerDay is the day of a date answer, YYYY-MM-DD, with any time of day dropped.
func AnswerDay(answer string) string {
	if HasTimeOfDay(answer) {
		return answer[:10]
	}
	return answer
}

// ComparableDateAnswers is getComparableDateAnswers: a day is compared with a date and time by the day alone, two values of the same kind as they are.
func ComparableDateAnswers(left, right string) (string, string) {
	if HasTimeOfDay(left) == HasTimeOfDay(right) {
		return left, right
	}
	return AnswerDay(left), AnswerDay(right)
}
