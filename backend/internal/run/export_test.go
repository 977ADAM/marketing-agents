package run

import ()

// ComputePercent — шов для тестов расчёта процента готовности фазы.
func ComputePercent(ph Phase, done, total int) int { return computePercent(ph, done, total) }
