package domain

const (
	defaultSlideConcurrency = 2
	maxSlideConcurrency     = 10
	sourceSystem            = "system"
	sourceGroup             = "group"
	sourceUser              = "user"
)

// ClampSlideConcurrency keeps concurrency within safe runtime bounds.
func ClampSlideConcurrency(limit, slideCount int) int {
	if limit < 1 {
		limit = defaultSlideConcurrency
	}
	if limit > maxSlideConcurrency {
		limit = maxSlideConcurrency
	}
	if slideCount > 0 && limit > slideCount {
		limit = slideCount
	}
	if limit < 1 {
		return 1
	}
	return limit
}

// ResolveSlideConcurrency picks user override > group > system default, then clamps.
func ResolveSlideConcurrency(userLimit *int, groupLimit, systemLimit, slideCount int) (limit int, source string) {
	limit = systemLimit
	source = sourceSystem
	if groupLimit > 0 {
		limit = groupLimit
		source = sourceGroup
	}
	if userLimit != nil {
		limit = *userLimit
		source = sourceUser
	}
	return ClampSlideConcurrency(limit, slideCount), source
}
