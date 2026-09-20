// Package stability 实现小倾角初稳性的核心计算。
//
// 计算链条：
//
//	BM = IT / ∇                横稳心半径（m）
//	GM = KB + BM − KG          初稳性高度（m），为正则初稳性为正
//	GZ = GM · sinφ             复原力臂（m），φ 为横倾角
//	M  = ρ · ∇ · g · GZ        复原力矩（N·m），排水质量取 ρ∇
//
// 包内所有角度量一律以弧度计；度 → 弧度的换算集中在 DegToRad，
// 任何以度计的横倾角必须先经 DegToRad 换算再进入三角函数。
package stability

import (
	"fmt"
	"math"
	"strings"
)

const (
	// DefaultDensity 为默认水密度（海水），单位 kg/m³。
	DefaultDensity = 1025.0
	// DefaultGravity 为默认重力加速度，单位 m/s²。
	DefaultGravity = 9.81
	// SmallAngleLimitDeg 为小倾角近似的适用上限，单位为度。
	// 超过该角度仍按 GZ = GM·sinφ 给值，但结果附带越界提醒。
	SmallAngleLimitDeg = 10.0
)

// Input 描述一个装载状态的浮态几何参数。
type Input struct {
	Volume  float64 `json:"volume"`  // 排水体积 ∇，m³，必须为正
	KB      float64 `json:"kb"`      // 浮心距基线高度，m，不得为负
	KG      float64 `json:"kg"`      // 重心距基线高度，m，不得为负
	IT      float64 `json:"it"`      // 水线面对纵中剖面的横向惯性矩，m⁴，必须为正
	Density float64 `json:"density"` // 水密度 ρ，kg/m³；0 表示取默认值
	Gravity float64 `json:"gravity"` // 重力加速度 g，m/s²；0 表示取默认值
}

// Status 标注初稳性的正负。
type Status string

const (
	StatusPositive Status = "positive" // GM > 0，受扰后自行扶正
	StatusNegative Status = "negative" // GM < 0，初稳性丧失
	StatusNeutral  Status = "neutral"  // GM = 0，临界
)

// Result 是单次稳性核算的完整结果。
type Result struct {
	Input            Input    `json:"input"`            // 实际参与计算的参数（已套用默认值）
	BM               float64  `json:"bm"`               // 横稳心半径，m
	GM               float64  `json:"gm"`               // 初稳性高度，m
	GZ               float64  `json:"gz"`               // 复原力臂，m
	RightingMoment   float64  `json:"rightingMoment"`   // 复原力矩，N·m
	HeelAngleDeg     float64  `json:"heelAngleDeg"`     // 横倾角，度
	HeelAngleRad     float64  `json:"heelAngleRad"`     // 横倾角，弧度（实际参与三角函数的值）
	Stable           bool     `json:"stable"`           // GM > 0
	Status           Status   `json:"status"`           // 初稳性正 / 负 / 临界
	BeyondSmallAngle bool     `json:"beyondSmallAngle"` // 横倾角是否超出小倾角近似范围
	Warnings         []string `json:"warnings,omitempty"`
}

// ValidationError 汇总参数校验发现的全部问题。
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "浮态参数校验失败：" + strings.Join(e.Problems, "；")
}

// Validate 校验浮态参数是否满足物理约定，不满足时返回 *ValidationError。
func Validate(in Input) error {
	var p []string
	checkFinite := func(name string, v float64) bool {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			p = append(p, fmt.Sprintf("%s 必须为有限数值，当前值 %v", name, v))
			return false
		}
		return true
	}
	if checkFinite("排水体积 volume", in.Volume) && in.Volume <= 0 {
		p = append(p, fmt.Sprintf("排水体积 ∇ (volume) 必须为正，当前值 %v m³", in.Volume))
	}
	if checkFinite("水线面横向惯性矩 it", in.IT) && in.IT <= 0 {
		p = append(p, fmt.Sprintf("水线面横向惯性矩 IT (it) 必须为正，当前值 %v m⁴", in.IT))
	}
	if checkFinite("浮心高度 kb", in.KB) && in.KB < 0 {
		p = append(p, fmt.Sprintf("浮心距基线高度 KB (kb) 不得为负，当前值 %v m", in.KB))
	}
	if checkFinite("重心高度 kg", in.KG) && in.KG < 0 {
		p = append(p, fmt.Sprintf("重心距基线高度 KG (kg) 不得为负，当前值 %v m", in.KG))
	}
	if checkFinite("水密度 density", in.Density) && in.Density < 0 {
		p = append(p, fmt.Sprintf("水密度 ρ (density) 不得为负（0 表示取默认值），当前值 %v kg/m³", in.Density))
	}
	if checkFinite("重力加速度 gravity", in.Gravity) && in.Gravity < 0 {
		p = append(p, fmt.Sprintf("重力加速度 g (gravity) 不得为负（0 表示取默认值），当前值 %v m/s²", in.Gravity))
	}
	if len(p) > 0 {
		return &ValidationError{Problems: p}
	}
	return nil
}

// WithDefaults 返回套用了默认密度与重力加速度后的参数副本。
func (in Input) WithDefaults() Input {
	if in.Density == 0 {
		in.Density = DefaultDensity
	}
	if in.Gravity == 0 {
		in.Gravity = DefaultGravity
	}
	return in
}

// BM 计算横稳心半径：BM = IT / ∇，单位 m。
func BM(volume, it float64) float64 { return it / volume }

// GM 计算初稳性高度：GM = KB + BM − KG，单位 m。
func GM(kb, bm, kg float64) float64 { return kb + bm - kg }

// GZ 计算小倾角复原力臂：GZ = GM · sinφ，φ 以弧度计，单位 m。
func GZ(gm, phiRad float64) float64 { return gm * math.Sin(phiRad) }

// RightingMoment 计算复原力矩：M = ρ · ∇ · g · GZ，单位 N·m。
func RightingMoment(density, volume, gravity, gz float64) float64 {
	return density * volume * gravity * gz
}

// DegToRad 把角度从度换算为弧度。任何以度计的横倾角必须先经此换算再进入三角函数。
func DegToRad(deg float64) float64 { return deg * math.Pi / 180 }

// RadToDeg 把角度从弧度换算为度。
func RadToDeg(rad float64) float64 { return rad * 180 / math.Pi }

// RectangularWaterplaneIT 计算矩形水线面对纵中剖面的横向惯性矩：IT = B³·L / 12，单位 m⁴。
func RectangularWaterplaneIT(beam, length float64) float64 {
	return beam * beam * beam * length / 12
}

// Evaluate 完成一次稳性核算，heelRad 为横倾角（弧度）。
func Evaluate(in Input, heelRad float64) (Result, error) {
	if err := Validate(in); err != nil {
		return Result{}, err
	}
	if math.IsNaN(heelRad) || math.IsInf(heelRad, 0) {
		return Result{}, &ValidationError{Problems: []string{fmt.Sprintf("横倾角必须为有限数值，当前值 %v rad", heelRad)}}
	}

	in = in.WithDefaults()
	bm := BM(in.Volume, in.IT)
	gm := GM(in.KB, bm, in.KG)
	gz := GZ(gm, heelRad)

	res := Result{
		Input:          in,
		BM:             bm,
		GM:             gm,
		GZ:             gz,
		RightingMoment: RightingMoment(in.Density, in.Volume, in.Gravity, gz),
		HeelAngleDeg:   RadToDeg(heelRad),
		HeelAngleRad:   heelRad,
	}
	res.Status, res.Stable = statusOf(gm)

	switch res.Status {
	case StatusNegative:
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("GM = %.4f m 为负：初稳性丧失，船舶受扰后不会自行扶正", gm))
	case StatusNeutral:
		res.Warnings = append(res.Warnings, "GM = 0：初稳性处于临界状态")
	}
	if heelDeg := res.HeelAngleDeg; math.Abs(heelDeg) > SmallAngleLimitDeg {
		res.BeyondSmallAngle = true
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("横倾角 %.2f° 超出小倾角近似适用范围（±%.0f°），GZ = GM·sinφ 为外推结果，仅供参考",
				heelDeg, SmallAngleLimitDeg))
	}
	return res, nil
}

// EvaluateDeg 与 Evaluate 相同，但横倾角以度计；内部先换算为弧度再计算。
func EvaluateDeg(in Input, heelDeg float64) (Result, error) {
	if math.IsNaN(heelDeg) || math.IsInf(heelDeg, 0) {
		return Result{}, &ValidationError{Problems: []string{fmt.Sprintf("横倾角必须为有限数值，当前值 %v°", heelDeg)}}
	}
	return Evaluate(in, DegToRad(heelDeg))
}

func statusOf(gm float64) (Status, bool) {
	switch {
	case gm > 0:
		return StatusPositive, true
	case gm < 0:
		return StatusNegative, false
	default:
		return StatusNeutral, false
	}
}
