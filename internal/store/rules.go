package store

// ResolveQuotaLimits applies user override on top of group defaults.
func ResolveQuotaLimits(user User, group UserGroup) (pptLimit, slideLimit int, source string) {
	pptLimit = group.DailyPPTLimit
	slideLimit = group.DailySlideLimit
	source = QuotaSourceGroup
	if user.DailyPPTLimit != nil {
		pptLimit = *user.DailyPPTLimit
		source = QuotaSourceUser
	}
	if user.DailySlideLimit != nil {
		slideLimit = *user.DailySlideLimit
		source = QuotaSourceUser
	}
	return pptLimit, slideLimit, source
}

// BuildEffectiveQuota computes remaining slide quota for API responses.
// PPT remaining is intentionally reported as 0 (page quota is the active limit).
func BuildEffectiveQuota(date string, _ int, slideLimit int, source string, group UserGroup, usage DailyUsage) EffectiveQuota {
	remaining := slideLimit - usage.SlidesUsed - usage.SlidesReserved
	if remaining < 0 {
		remaining = 0
	}
	return EffectiveQuota{
		Date:            date,
		DailyPPTLimit:   0,
		DailySlideLimit: slideLimit,
		PPTUsed:         usage.PPTUsed,
		SlidesUsed:      usage.SlidesUsed,
		PPTReserved:     usage.PPTReserved,
		SlidesReserved:  usage.SlidesReserved,
		PPTRemaining:    0,
		SlidesRemaining: remaining,
		Source:          source,
		GroupID:         group.ID,
		GroupName:       group.Name,
	}
}
