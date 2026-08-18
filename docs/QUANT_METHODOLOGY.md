# Robin Quantitative Trading Platform — Quantitative Methodology & Modeling
**Document ID:** SPEC-QUANT-202608-01  
**Classification:** Quantitative Research & Modeling Specification  

---

## 1. Transaction Cost & Market Impact Modeling

### Square-Root Market Impact Law
To prevent unrealistic fill assumptions during backtesting:
$$\text{Impact} = Y \cdot \sigma \cdot \sqrt{\frac{V_{\text{order}}}{V_{\text{ADV}}}}$$
Where:
* $Y$: Dimensionless constant (typically $\approx 0.5 - 0.7$).
* $\sigma$: Daily asset volatility.
* $V_{\text{order}}$: Quantity of the order.
* $V_{\text{ADV}}$: Average daily volume.

### Latency Delay Penalty
Simulated fill prices account for execution latency jitter using an exponential delay distribution:
$$P_{\text{fill}} = P_{\text{mid}} \pm \left(\frac{\text{Spread}}{2} + \text{Slippage}_{\text{impact}} + \Delta_{\text{jitter}}\right)$$

---

## 2. Position Sizing & Kelly Criterion
Optimal capital allocation uses fractional Kelly with volatility scaling:
$$f^* = \min\left(f_{\text{max}}, \frac{\mu - r}{\sigma^2} \cdot \gamma\right)$$
Where $\gamma$ is a fractional dampener (e.g. 0.25 to 0.50) to mitigate model estimation risk.

---

## 3. Cross-Validation & Prevention of Backtest Overfitting
* **Purged Cross-Validation:** Eliminates label overlap between training and testing sets.
* **Embargo:** Adds a post-test cooling period to prevent information leakage from serially correlated financial returns.
* **Walk-Forward Analysis:** Sliding and expanding window parameter stability validation.
