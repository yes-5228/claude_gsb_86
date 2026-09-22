package conversion

import "github.com/drainage/desilting/internal/shared/option"

// BasisOptions 重量口径选项。
func BasisOptions() []option.Option {
	return option.List(
		BasisWet, "湿重",
		BasisDry, "干重",
	)
}

// BasisLabel 返回口径的中文名称。
func BasisLabel(basis string) string {
	return option.Label(BasisOptions(), basis)
}

// HasBasis 判断取值是否为合法的重量口径。
func HasBasis(basis string) bool {
	return option.Has(BasisOptions(), basis)
}
