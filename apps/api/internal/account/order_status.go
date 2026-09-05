package account

import "fmt"

var orderTransitions = map[string]map[string]bool{
	OrderPendingPayment: {
		OrderPaid:     true,
		OrderCanceled: true,
	},
	OrderPaid: {
		OrderShipped:  true,
		OrderCanceled: true,
	},
	OrderShipped: {
		OrderCompleted: true,
	},
}

func CanTransitionOrder(from, to string) bool {
	return orderTransitions[from][to]
}

func ValidateOrderTransition(from, to string) error {
	if !CanTransitionOrder(from, to) {
		return fmt.Errorf("invalid order status transition %q -> %q", from, to)
	}
	return nil
}
