package durable

import (
	"encoding/json"
	"fmt"
	"math"
)

type scanPosition struct {
	order ScanOrder
	after *Id
}

func scanStart(requested ScanOrder, cursor Cursor, fallback ScanOrder) (scanPosition, error) {
	if requested != "" && requested != ScanOrderAscending && requested != ScanOrderDescending {
		return scanPosition{}, fmt.Errorf("Invalid scan order: %s", requested)
	}
	if cursor == nil {
		if requested == "" {
			requested = fallback
		}
		return scanPosition{order: requested}, nil
	}

	rawAfter, ok := cursor["after"]
	if !ok || len(rawAfter) == 0 {
		return scanPosition{}, fmt.Errorf("Invalid storage cursor")
	}
	var value any
	if err := json.Unmarshal(rawAfter, &value); err != nil {
		return scanPosition{}, fmt.Errorf("Invalid storage cursor")
	}
	after, ok := value.(float64)
	if !ok || math.Trunc(after) != after || math.Abs(after) > float64(MaxSafeInteger) {
		return scanPosition{}, fmt.Errorf("Invalid storage cursor")
	}

	order := fallback
	if rawOrder, present := cursor["order"]; present {
		var stored string
		if err := json.Unmarshal(rawOrder, &stored); err != nil ||
			(stored != string(ScanOrderAscending) && stored != string(ScanOrderDescending)) {
			return scanPosition{}, fmt.Errorf("Invalid storage cursor")
		}
		order = ScanOrder(stored)
	}
	if requested != "" && requested != order {
		return scanPosition{}, fmt.Errorf("The cursor continues a %s scan; the query asks for %s", order, requested)
	}
	afterID := Id(after)
	return scanPosition{order: order, after: &afterID}, nil
}

// D213: Keep first-page SQL scans unbounded instead of using upstream's
// -1/MAX_SAFE_INTEGER sentinels; the maximum safe ID remains visible.
func scanSQL(start scanPosition) (clause string, after Id, direction string) {
	direction = "ASC"
	if start.order == ScanOrderDescending {
		direction = "DESC"
	}
	if start.after == nil {
		return "", 0, direction
	}
	if start.order == ScanOrderAscending {
		return "id > ?", *start.after, direction
	}
	return "id < ?", *start.after, direction
}

func cursorAfterOrder(id Id, order ScanOrder) Cursor {
	after, _ := json.Marshal(id)
	encodedOrder, _ := json.Marshal(order)
	return Cursor{"after": after, "order": encodedOrder}
}

func pageOfOrder[T any](values []T, limit int, idOf func(T) Id, order ScanOrder) Page[T] {
	count := min(limit, len(values))
	items := make([]T, 0, count)
	for _, value := range values[:count] {
		items = append(items, cloneValue(value))
	}
	result := Page[T]{Items: items}
	if len(values) > limit {
		result.Next = cursorAfterOrder(idOf(items[len(items)-1]), order)
	}
	return result
}

func scanIndexRange(ids []Id, start scanPosition) (index, end, step int) {
	if start.order == ScanOrderAscending {
		index = 0
		if start.after != nil {
			index = upperBound(ids, *start.after)
		}
		return index, len(ids), 1
	}
	end = len(ids)
	if start.after != nil {
		end = lowerBound(ids, *start.after)
	}
	return end - 1, -1, -1
}
