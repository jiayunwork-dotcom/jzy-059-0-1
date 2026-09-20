package stability

import (
	"errors"
	"math"
	"strings"
	"testing"
)

const tol = 1e-9

func approxEq(a, b float64) bool {
	return math.Abs(a-b) <= tol*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

// 基准浮态：BM = 2000/1000 = 2 m，GM = 2 + 2 − 3 = 1 m（正稳性）。
func baseInput() Input {
	return Input{Volume: 1000, KB: 2, KG: 3, IT: 2000}
}

func TestBMGMValues(t *testing.T) {
	res, err := EvaluateDeg(baseInput(), 5)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	if !approxEq(res.BM, 2.0) {
		t.Errorf("BM = %v，期望 2.0", res.BM)
	}
	if !approxEq(res.GM, 1.0) {
		t.Errorf("GM = %v，期望 1.0", res.GM)
	}
	if !res.Stable || res.Status != StatusPositive {
		t.Errorf("GM>0 应判定为正稳性，实际 stable=%v status=%v", res.Stable, res.Status)
	}
}

// 只把 IT 加倍：BM 加倍，且 BM 与 GM 的增量完全相同（ΔGM = ΔBM = ΔIT/∇）。
func TestDoublingITRaisesBMAndGMBySameIncrement(t *testing.T) {
	in := baseInput()
	r1, err := EvaluateDeg(in, 5)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	in.IT *= 2
	r2, err := EvaluateDeg(in, 5)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	if !approxEq(r2.BM, 2*r1.BM) {
		t.Errorf("IT 加倍后 BM = %v，期望 %v", r2.BM, 2*r1.BM)
	}
	dBM, dGM := r2.BM-r1.BM, r2.GM-r1.GM
	if !approxEq(dBM, dGM) {
		t.Errorf("ΔBM = %v 与 ΔGM = %v 应相等", dBM, dGM)
	}
	if !approxEq(dGM, in.IT/2/in.Volume) { // 增量恰为原 IT/∇
		t.Errorf("ΔGM = %v，期望 %v（= 原 IT/∇）", dGM, in.IT/2/in.Volume)
	}
}

// 只抬高 KG：GM 随 KG 线性下降；KG 超过 KB+BM 后 GM 转负、判定初稳性负。
func TestRaisingKGLowersGMLinearly(t *testing.T) {
	in := baseInput()
	r0, err := EvaluateDeg(in, 5)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	gm0 := r0.GM
	for _, delta := range []float64{0.1, 0.5, 1.0, 2.5} {
		in2 := in
		in2.KG += delta
		r, err := EvaluateDeg(in2, 5)
		if err != nil {
			t.Fatalf("EvaluateDeg 失败: %v", err)
		}
		if !approxEq(r.GM, gm0-delta) {
			t.Errorf("KG 抬高 %v 后 GM = %v，期望 %v", delta, r.GM, gm0-delta)
		}
	}

	// KG 恰好等于 KB+BM：GM = 0，临界。
	crit := in
	crit.KG = in.KB + r0.BM
	rc, err := EvaluateDeg(crit, 5)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	if !approxEq(rc.GM, 0) || rc.Status != StatusNeutral {
		t.Errorf("KG=KB+BM 时 GM = %v、status = %v，期望 0 / neutral", rc.GM, rc.Status)
	}

	// KG 超过 KB+BM：GM 转负，判定初稳性负。
	neg := in
	neg.KG = in.KB + r0.BM + 0.5
	rn, err := EvaluateDeg(neg, 5)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	if rn.GM >= 0 || rn.Stable || rn.Status != StatusNegative {
		t.Errorf("KG 超过 KB+BM 后 GM = %v、stable = %v、status = %v，期望负稳性", rn.GM, rn.Stable, rn.Status)
	}
	if len(rn.Warnings) == 0 {
		t.Error("负稳性应附带提醒")
	}
}

// 横倾角为零时复原力臂恒为零（无论 GM 正负），复原力矩也为零。
func TestZeroHeelGivesZeroGZ(t *testing.T) {
	for _, in := range []Input{baseInput(), {Volume: 1000, KB: 2, KG: 9, IT: 2000}} {
		res, err := EvaluateDeg(in, 0)
		if err != nil {
			t.Fatalf("EvaluateDeg 失败: %v", err)
		}
		if res.GZ != 0 {
			t.Errorf("零横倾时 GZ = %v，期望恰为 0", res.GZ)
		}
		if res.RightingMoment != 0 {
			t.Errorf("零横倾时复原力矩 = %v，期望恰为 0", res.RightingMoment)
		}
	}
}

// 矩形驳船船宽加倍：水线面惯性矩变为 8 倍（IT ∝ B³），同排水体积下 BM 同步变为 8 倍。
func TestBargeBeamDoublingScalesITAndBM(t *testing.T) {
	const length, beam, draft = 40.0, 10.0, 2.0

	it1 := RectangularWaterplaneIT(beam, length)
	it2 := RectangularWaterplaneIT(2*beam, length)
	if !approxEq(it2, 8*it1) {
		t.Errorf("船宽加倍后 IT = %v，期望 8 倍即 %v", it2, 8*it1)
	}

	// 排水体积不变（只改水线面）：BM 变为 8 倍。
	vol := length * beam * draft
	bm1, bm2 := BM(vol, it1), BM(vol, it2)
	if !approxEq(bm2, 8*bm1) {
		t.Errorf("同排水体积下 BM = %v，期望 8 倍即 %v", bm2, 8*bm1)
	}

	// 完整几何（同吃水，排水体积随船宽加倍）：BM 仍抬升为 4 倍。
	bmWide := BM(2*vol, it2)
	if !approxEq(bmWide, 4*bm1) {
		t.Errorf("船宽、排水体积同步加倍后 BM = %v，期望 4 倍即 %v", bmWide, 4*bm1)
	}
	if bmWide <= bm1 {
		t.Errorf("船宽加倍后 BM 应大幅抬升：%v -> %v", bm1, bmWide)
	}
}

// 改变水密度只影响复原力矩的牛顿数值，不影响以米计的 GM 与 GZ。
func TestDensityAffectsOnlyMoment(t *testing.T) {
	in := baseInput()
	r1, err := EvaluateDeg(in, 8)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	in.Density = 1000 // 淡水
	r2, err := EvaluateDeg(in, 8)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	if !approxEq(r1.GM, r2.GM) || !approxEq(r1.GZ, r2.GZ) {
		t.Errorf("密度变化不应影响 GM/GZ：(%v, %v) vs (%v, %v)", r1.GM, r1.GZ, r2.GM, r2.GZ)
	}
	want := r1.RightingMoment * (1000.0 / DefaultDensity)
	if !approxEq(r2.RightingMoment, want) {
		t.Errorf("复原力矩应随密度等比变化：%v，期望 %v", r2.RightingMoment, want)
	}
}

// 角度单位换算：度必须先换成弧度再进三角函数。
// 若漏掉换算，10° 会被当成 10 rad，sin(10) < 0，正稳性船会算出负的复原力臂。
func TestDegreeToRadianConversion(t *testing.T) {
	if !approxEq(DegToRad(180), math.Pi) {
		t.Errorf("DegToRad(180) = %v，期望 π", DegToRad(180))
	}
	if !approxEq(DegToRad(10), math.Pi/18) {
		t.Errorf("DegToRad(10) = %v，期望 π/18", DegToRad(10))
	}

	in := baseInput() // GM = 1 m
	res, err := EvaluateDeg(in, 10)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	want := res.GM * math.Sin(math.Pi/18)
	if !approxEq(res.GZ, want) {
		t.Errorf("10° 时 GZ = %v，期望 GM·sin(π/18) = %v", res.GZ, want)
	}
	if res.GZ <= 0 {
		t.Errorf("正稳性船在 10° 横倾时 GZ 应为正，实际 %v（疑似角度未换算成弧度）", res.GZ)
	}
	if wrong := res.GM * math.Sin(10); math.Abs(res.GZ-wrong) < 1e-6 {
		t.Errorf("GZ = %v 与 sin(10 rad) 的结果一致，角度单位换算疑似被跳过", res.GZ)
	}
	if !approxEq(res.HeelAngleRad, math.Pi/18) {
		t.Errorf("结果中的弧度值 = %v，期望 π/18", res.HeelAngleRad)
	}

	// 度入口与弧度入口必须给出相同结果。
	rad, err := Evaluate(in, math.Pi/18)
	if err != nil {
		t.Fatalf("Evaluate 失败: %v", err)
	}
	if !approxEq(res.GZ, rad.GZ) || !approxEq(res.RightingMoment, rad.RightingMoment) {
		t.Errorf("度/弧度入口结果不一致：GZ %v vs %v", res.GZ, rad.GZ)
	}
}

// 复原力矩的数值核对：M = ρ·∇·g·GZ。
func TestRightingMomentValue(t *testing.T) {
	in := baseInput()
	res, err := EvaluateDeg(in, 10)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	want := DefaultDensity * in.Volume * DefaultGravity * res.GZ
	if !approxEq(res.RightingMoment, want) {
		t.Errorf("复原力矩 = %v，期望 ρ·∇·g·GZ = %v", res.RightingMoment, want)
	}
}

// 超出小倾角范围仍按公式给值，但必须带越界提醒。
func TestBeyondSmallAngleWarning(t *testing.T) {
	in := baseInput()
	res, err := EvaluateDeg(in, 30)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	if !res.BeyondSmallAngle {
		t.Error("30° 应标记为超出小倾角范围")
	}
	if !approxEq(res.GZ, res.GM*math.Sin(DegToRad(30))) {
		t.Errorf("越界时仍应按 GZ=GM·sinφ 给值，实际 %v", res.GZ)
	}
	joined := strings.Join(res.Warnings, " ")
	if !strings.Contains(joined, "小倾角") {
		t.Errorf("越界结果应附小倾角提醒，实际 warnings=%v", res.Warnings)
	}

	ok, err := EvaluateDeg(in, 5)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	if ok.BeyondSmallAngle || len(ok.Warnings) != 0 {
		t.Errorf("5° 不应有越界标记或提醒，实际 beyond=%v warnings=%v", ok.BeyondSmallAngle, ok.Warnings)
	}
}

// 非法参数必须被挡在计算之外，并说明原因。
func TestValidationRejectsUnphysicalInput(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want string
	}{
		{"排水体积为零", Input{Volume: 0, KB: 2, KG: 3, IT: 2000}, "排水体积"},
		{"排水体积为负", Input{Volume: -100, KB: 2, KG: 3, IT: 2000}, "排水体积"},
		{"惯性矩为零", Input{Volume: 1000, KB: 2, KG: 3, IT: 0}, "惯性矩"},
		{"惯性矩为负", Input{Volume: 1000, KB: 2, KG: 3, IT: -5}, "惯性矩"},
		{"浮心高度为负", Input{Volume: 1000, KB: -0.1, KG: 3, IT: 2000}, "KB"},
		{"重心高度为负", Input{Volume: 1000, KB: 2, KG: -3, IT: 2000}, "KG"},
		{"密度为负", Input{Volume: 1000, KB: 2, KG: 3, IT: 2000, Density: -1}, "密度"},
		{"重力加速度为负", Input{Volume: 1000, KB: 2, KG: 3, IT: 2000, Gravity: -9.8}, "重力加速度"},
		{"排水体积 NaN", Input{Volume: math.NaN(), KB: 2, KG: 3, IT: 2000}, "有限"},
		{"惯性矩 Inf", Input{Volume: 1000, KB: 2, KG: 3, IT: math.Inf(1)}, "有限"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := EvaluateDeg(tc.in, 5)
			if err == nil {
				t.Fatalf("期望校验失败，实际通过")
			}
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("期望 *ValidationError，实际 %T", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息 %q 应包含 %q", err.Error(), tc.want)
			}
		})
	}

	if _, err := EvaluateDeg(baseInput(), math.NaN()); err == nil {
		t.Error("横倾角为 NaN 应被拒绝")
	}
}

// 密度与重力加速度缺省时套用默认值。
func TestDefaultsApplied(t *testing.T) {
	res, err := EvaluateDeg(baseInput(), 5)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	if res.Input.Density != DefaultDensity || res.Input.Gravity != DefaultGravity {
		t.Errorf("缺省密度/重力应套用默认值，实际 ρ=%v g=%v", res.Input.Density, res.Input.Gravity)
	}
}
