package analysis

// MergeLLMFindings incrementally folds completed LLM findings into an existing
// deterministic or partially reviewed result without duplicating stable IDs.
func MergeLLMFindings(existing, additions []Finding) []Finding {
	merged := append([]Finding(nil), existing...)
	for _, addition := range additions {
		if addition.Type == FindingOverlap {
			merged = mergeLLMOverlapFindings(merged, []Finding{addition})
			continue
		}
		replaced := false
		for i := range merged {
			if merged[i].ID != addition.ID {
				continue
			}
			merged[i] = addition
			replaced = true
			break
		}
		if !replaced {
			merged = append(merged, addition)
		}
	}
	SortFindings(merged)
	return merged
}
