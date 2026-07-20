package service

import "testing"

func TestConsumptionConcurrencyTier(t *testing.T) {
	tests := []struct {
		name string
		cost float64
		want int
	}{
		{name: "below first tier", cost: 49.999999, want: 0},
		{name: "first tier", cost: 50, want: 1},
		{name: "second tier", cost: 100, want: 2},
		{name: "third tier", cost: 200, want: 3},
		{name: "fourth tier", cost: 500, want: 4},
		{name: "above fourth tier", cost: 900, want: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConsumptionConcurrencyTier(tt.cost); got != tt.want {
				t.Fatalf("ConsumptionConcurrencyTier(%v) = %d, want %d", tt.cost, got, tt.want)
			}
		})
	}
}

func TestConsumptionConcurrencyBonus(t *testing.T) {
	if got := ConsumptionConcurrencyBonus(4); got != 16 {
		t.Fatalf("ConsumptionConcurrencyBonus(4) = %d, want 16", got)
	}
}
