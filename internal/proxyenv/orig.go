package proxyenv

// origVal 表示 Apply 之前某个环境变量是否存在及其取值。
type origVal struct {
	WasSet bool
	Value  string
}
