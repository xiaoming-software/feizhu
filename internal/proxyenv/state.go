package proxyenv

import "sync"

var mu sync.Mutex
var saved map[string]origVal
