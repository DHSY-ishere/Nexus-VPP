package settlement

// Pool settlement moves money for one zone-interval after the delivery check has
// decided who really delivered and who really received. Escrow is held per trade
// (Hold), closed for the confirmed charge (Close, the rest goes back to the buyer),
// and the charged money is paid out to sellers (Pay) in proportion to what each
// seller was shown to have delivered. Sum of charges equals sum of payouts, so no
// money is created or destroyed.

// Close ends a trade's escrow: charge is kept for the sellers, the remainder is
// refunded to the buyer. It returns the charge actually taken.
func (l *Ledger) Close(tradeID string, charge float64) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.held[tradeID]
	if !ok {
		return 0
	}
	if charge > h.amount {
		charge = h.amount
	}
	if charge < 0 {
		charge = 0
	}
	l.Balances[h.buyer] += h.amount - charge
	delete(l.held, tradeID)
	return charge
}

// Pay credits a seller with money taken from buyers, after the solidarity levy.
// It returns the levy.
func (l *Ledger) Pay(seller string, amount, levyRate float64) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	levy := amount * levyRate
	l.Balances[seller] += amount - levy
	l.Pool += levy
	return levy
}

// PoolID is the Transfer destination that means "the solidarity pool".
const PoolID = "POOL"

// Transfer moves money between wallets (penalties and compensation). to == PoolID
// pays the solidarity pool.
func (l *Ledger) Transfer(from, to string, amount float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Balances[from] -= amount
	if to == PoolID {
		l.Pool += amount
		return
	}
	l.Balances[to] += amount
}
