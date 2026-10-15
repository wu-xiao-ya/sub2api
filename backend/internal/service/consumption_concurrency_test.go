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
	tests := []struct {
		tier int
		want int
	}{
		{tier: -1, want: 0},
		{tier: 0, want: 0},
		{tier: 1, want: 5},
		{tier: 2, want: 10},
		{tier: 3, want: 20},
		{tier: 4, want: 30},
		{tier: 9, want: 30},
	}
	for _, tt := range tests {
		if got := ConsumptionConcurrencyBonus(tt.tier); got != tt.want {
			t.Fatalf("ConsumptionConcurrencyBonus(%d) = %d, want %d", tt.tier, got, tt.want)
		}
	}
}
