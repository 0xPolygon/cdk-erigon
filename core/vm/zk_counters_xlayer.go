package vm

func NewEmptyCounters() Counters {
	array := make(Counters, CounterTypesCount)

	for i := range array {
		array[i] = &Counter{}
	}

	return array
}

func GetDifferUsedAsMap(newerCounters, oldderCounters Counters) map[string]int {
	return map[string]int{
		string(CounterKeyNames[S]):   newerCounters[S].used - oldderCounters[S].used,
		string(CounterKeyNames[A]):   newerCounters[A].used - oldderCounters[A].used,
		string(CounterKeyNames[B]):   newerCounters[B].used - oldderCounters[B].used,
		string(CounterKeyNames[M]):   newerCounters[M].used - oldderCounters[M].used,
		string(CounterKeyNames[K]):   newerCounters[K].used - oldderCounters[K].used,
		string(CounterKeyNames[D]):   newerCounters[D].used - oldderCounters[D].used,
		string(CounterKeyNames[P]):   newerCounters[P].used - oldderCounters[P].used,
		string(CounterKeyNames[SHA]): newerCounters[SHA].used - oldderCounters[SHA].used,
	}
}
