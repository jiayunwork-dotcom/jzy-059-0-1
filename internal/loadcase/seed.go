package loadcase

import "stability-service/internal/stability"

// DemoBargeName 是预置矩形驳船算例的装载状态名。
const DemoBargeName = "demo-barge"

// DemoBarge 构造预置的矩形驳船算例，主要尺度与手算结果：
//
//	船长 L = 40 m，船宽 B = 10 m，吃水 T = 2 m
//	排水体积 ∇ = L·B·T = 800 m³
//	浮心高度 KB = T/2 = 1.0 m
//	水线面横向惯性矩 IT = B³·L/12 = 10³·40/12 = 3333.33 m⁴
//	横稳心半径 BM = IT/∇ = 4.1667 m
//	重心高度 KG = 3.0 m
//	初稳性高度 GM = KB + BM − KG = 2.1667 m > 0（正稳性）
func DemoBarge() Case {
	const length, beam, draft = 40.0, 10.0, 2.0
	return Case{
		Name:        DemoBargeName,
		Description: "矩形驳船示范算例：L=40m B=10m T=2m，IT=B³L/12=3333.33m⁴，BM=4.1667m，GM=2.1667m（正稳性）",
		Input: stability.Input{
			Volume:  length * beam * draft,
			KB:      draft / 2,
			KG:      3.0,
			IT:      stability.RectangularWaterplaneIT(beam, length),
			Density: stability.DefaultDensity,
			Gravity: stability.DefaultGravity,
		},
		Particulars: map[string]float64{
			"length": length,
			"beam":   beam,
			"draft":  draft,
		},
	}
}

// SeedDemoBarge 在存储中预置示范驳船算例；同名档已存在时保持不变。
func SeedDemoBarge(s *Store) error {
	if _, ok := s.Get(DemoBargeName); ok {
		return nil
	}
	return s.Create(DemoBarge())
}
