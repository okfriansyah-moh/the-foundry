package cost

type MonthlyStatement struct {
	MissionID          string
	RevenueUSD         float64
	CostUSD            float64
	GrossMarginUSD     float64
	NetContributionUSD float64
	Cycles             int
	PaybackDays        float64
}

func BuildMonthlyStatement(missionID string, revenueUSD, costUSD float64, cycles int, paybackDays float64) MonthlyStatement {
	grossMargin := revenueUSD - costUSD
	return MonthlyStatement{MissionID: missionID, RevenueUSD: revenueUSD, CostUSD: costUSD, GrossMarginUSD: grossMargin, NetContributionUSD: grossMargin, Cycles: cycles, PaybackDays: paybackDays}
}
