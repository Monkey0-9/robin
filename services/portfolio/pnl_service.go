package portfolio

// Portfolio is an alias to PortfolioManager for backwards compatibility.
type Portfolio = PortfolioManager

func NewPortfolio(initialCash float64) *Portfolio {
	return NewPortfolioManager(initialCash)
}
