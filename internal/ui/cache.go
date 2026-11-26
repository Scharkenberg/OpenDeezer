package ui

// cacheSizesMB are the stream cache budgets the Stream cache setting cycles through.
var cacheSizesMB = []int{0, 1 << 10, 2 << 10, 5 << 10, 10 << 10, 20 << 10}
