package scan

import (
	"math"
	"strings"
	"testing"

	"stability-service/internal/stability"
)

const tol = 1e-9

func approxEq(a, b float64) bool {
	return math.Abs(a-b) <= tol*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

func baseInput() stability.Input {
	return stability.Input{Volume: 1000, KB: 2, KG: 3, IT: 2000} // GM = 1 m
}

// 曲线从零度出发：首点 GZ 恒为零，正稳性下随后单调上升。
func TestSweepStartsAtZeroAndRises(t *testing.T) {
	curve, err := Sweep(baseInput(), 0, 10, 1)
	if err != nil {
		t.Fatalf("Sweep 失败: %v", err)
	}
	if len(curve.Points) != 11 {
		t.Fatalf("0..10 步长 1 应有 11 个点，实际 %d", len(curve.Points))
	}
	if curve.Points[0].AngleDeg != 0 || curve.Points[0].GZ != 0 {
		t.Errorf("首点应为 (0°, GZ=0)，实际 (%v°, %v)", curve.Points[0].AngleDeg, curve.Points[0].GZ)
	}
	for i := 1; i < len(curve.Points); i++ {
		if curve.Points[i].GZ <= curve.Points[i-1].GZ {
			t.Errorf("正稳性下 GZ 应单调上升：第 %d 点 %v 不大于第 %d 点 %v",
				i, curve.Points[i].GZ, i-1, curve.Points[i-1].GZ)
		}
	}
	if !curve.Stable || curve.Status != stability.StatusPositive {
		t.Errorf("曲线级判定应为正稳性，实际 stable=%v status=%v", curve.Stable, curve.Status)
	}
}

// 曲线上每个点都必须由稳性内核公式实算得来，与内核逐点核对。
func TestSweepPointsMatchKernel(t *testing.T) {
	in := baseInput()
	curve, err := Sweep(in, 0, 10, 0.5)
	if err != nil {
		t.Fatalf("Sweep 失败: %v", err)
	}
	for _, p := range curve.Points {
		res, err := stability.EvaluateDeg(in, p.AngleDeg)
		if err != nil {
			t.Fatalf("EvaluateDeg 失败: %v", err)
		}
		if !approxEq(p.GZ, res.GZ) || !approxEq(p.RightingMoment, res.RightingMoment) {
			t.Errorf("角度 %v°：扫描点 (GZ=%v, M=%v) 与内核 (GZ=%v, M=%v) 不一致",
				p.AngleDeg, p.GZ, p.RightingMoment, res.GZ, res.RightingMoment)
		}
		if !approxEq(p.AngleRad, stability.DegToRad(p.AngleDeg)) {
			t.Errorf("角度 %v° 的弧度值 %v 与 DegToRad 不一致", p.AngleDeg, p.AngleRad)
		}
		if !approxEq(p.GZ, curve.GM*math.Sin(p.AngleRad)) {
			t.Errorf("角度 %v°：GZ = %v 不等于 GM·sinφ = %v", p.AngleDeg, p.GZ, curve.GM*math.Sin(p.AngleRad))
		}
	}
}

// 步长不能整除区间时，终点仍精确落在 toDeg。
func TestSweepIncludesExactEndpoints(t *testing.T) {
	curve, err := Sweep(baseInput(), 0, 10, 3)
	if err != nil {
		t.Fatalf("Sweep 失败: %v", err)
	}
	want := []float64{0, 3, 6, 9, 10}
	if len(curve.Points) != len(want) {
		t.Fatalf("期望 %d 个点，实际 %d", len(want), len(curve.Points))
	}
	for i, w := range want {
		if !approxEq(curve.Points[i].AngleDeg, w) {
			t.Errorf("第 %d 点角度 = %v，期望 %v", i, curve.Points[i].AngleDeg, w)
		}
	}
}

// 扫描区间越界时：仍给值，但相应点带越界标记，曲线级附提醒。
func TestSweepBeyondSmallAngle(t *testing.T) {
	curve, err := Sweep(baseInput(), 0, 30, 5)
	if err != nil {
		t.Fatalf("Sweep 失败: %v", err)
	}
	var beyond int
	for _, p := range curve.Points {
		if p.BeyondSmallAngle {
			beyond++
		}
		if !approxEq(p.GZ, curve.GM*math.Sin(p.AngleRad)) {
			t.Errorf("越界点仍应按公式给值：%v° GZ=%v", p.AngleDeg, p.GZ)
		}
	}
	if beyond == 0 {
		t.Error("0..30° 扫描应包含越界点")
	}
	joined := strings.Join(curve.Warnings, " ")
	if !strings.Contains(joined, "小倾角") {
		t.Errorf("曲线应附小倾角越界提醒，实际 warnings=%v", curve.Warnings)
	}
}

// 负稳性装载的曲线：GZ 随角度变负。
func TestSweepNegativeGM(t *testing.T) {
	in := stability.Input{Volume: 1000, KB: 2, KG: 9, IT: 2000} // GM = -6 m
	curve, err := Sweep(in, 0, 10, 5)
	if err != nil {
		t.Fatalf("Sweep 失败: %v", err)
	}
	if curve.Stable || curve.Status != stability.StatusNegative {
		t.Errorf("应判定为负稳性，实际 stable=%v status=%v", curve.Stable, curve.Status)
	}
	if curve.Points[0].GZ != 0 {
		t.Errorf("零横倾时 GZ 仍应为 0，实际 %v", curve.Points[0].GZ)
	}
	for i := 1; i < len(curve.Points); i++ {
		if curve.Points[i].GZ >= 0 {
			t.Errorf("负稳性下 %v° 的 GZ 应为负，实际 %v", curve.Points[i].AngleDeg, curve.Points[i].GZ)
		}
	}
}

// 非法扫描参数与非法浮态参数都要被拦截。
func TestSweepValidation(t *testing.T) {
	in := baseInput()
	if _, err := Sweep(in, 0, 10, 0); err == nil {
		t.Error("步长为 0 应被拒绝")
	}
	if _, err := Sweep(in, 0, 10, -1); err == nil {
		t.Error("步长为负应被拒绝")
	}
	if _, err := Sweep(in, 10, 0, 1); err == nil {
		t.Error("终点不大于起点应被拒绝")
	}
	if _, err := Sweep(in, 0, 360, 1e-6); err == nil {
		t.Error("点数超过上限应被拒绝")
	}
	if _, err := Sweep(in, math.NaN(), 10, 1); err == nil {
		t.Error("NaN 起点应被拒绝")
	}
	bad := stability.Input{Volume: -1, KB: 2, KG: 3, IT: 2000}
	if _, err := Sweep(bad, 0, 10, 1); err == nil {
		t.Error("非法浮态参数应被拒绝")
	}
}
