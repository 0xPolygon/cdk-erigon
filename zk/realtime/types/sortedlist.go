package types

import "slices"

type OrderedList[T any] struct {
	list        []T
	isOrdered   bool
	compareFunc func(a, b T) int
}

func NewOrderedList[T any](size int, compareFunc func(a, b T) int) *OrderedList[T] {
	return &OrderedList[T]{
		list:        make([]T, 0, size),
		isOrdered:   false,
		compareFunc: compareFunc,
	}
}

func (l *OrderedList[T]) Add(item T) {
	l.isOrdered = false
	l.list = append(l.list, item)
}

func (l *OrderedList[T]) Sort() {
	slices.SortFunc(l.list, l.compareFunc)
	l.isOrdered = true
}

func (l *OrderedList[T]) containsBinarySearch(item T) bool {
	upper := len(l.list)
	lower := 0
	for lower < upper {
		mid := (upper + lower) / 2
		cmp := l.compareFunc(item, l.list[mid])
		if cmp == 0 {
			return true
		}
		if cmp > 0 {
			lower = mid + 1
		} else {
			upper = mid
		}
	}
	return false
}

func (l *OrderedList[T]) containsLinear(item T) bool {
	for _, i := range l.list {
		if l.compareFunc(i, item) == 0 {
			return true
		}
	}
	return false
}

func (l *OrderedList[T]) Contains(item T) bool {
	if !l.isOrdered {
		l.containsLinear(item)
	}
	return l.containsBinarySearch(item)
}

func (l *OrderedList[T]) Size() int {
	return len(l.list)
}

func (l *OrderedList[T]) Items() []T {
	return l.list
}

func (l *OrderedList[T]) Clear() {
	l.isOrdered = false
	l.list = l.list[:0]
}

func (l *OrderedList[T]) SetItems(items []T) {
	l.list = items
	l.isOrdered = false
}
