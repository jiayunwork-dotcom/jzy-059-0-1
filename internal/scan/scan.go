// Package scan 在给定横倾角区间内逐点实算复原力臂，生成 GZ 随角度变化的曲线。
package scan

import (
	"fmt"
	"math"

	"stability-service/internal/stability"
)

// MaxPoints 限制单次扫描返回的点数上限，防止步长过小拖垮服务。
const MaxPoints = 10001

// Point 是 GZ 曲线上的一个点，全部字段由稳性内核实算得出。
type Point struct {
	AngleDeg         float64 `json:"angleDeg"`
	AngleRad         float64 `json:"angleRad"`
	GZ               float64 `json:"gz"`
	RightingMoment   float64 `json:"rightingMoment"`
	BeyondSmallAngle bool    `json:"beyondSmallAngle"`
}

// Curve 是一次横倾扫描的完整结果。
type Curve struct {
	BM       float64          `json:"bm"`
	GM       float64          `json:"gm"`
	Stable   bool             `json:"stable"`
	Status   stability.Status `json:"status"`
	FromDeg  float64          `json:"fromDeg"`
	ToDeg    float64          `json:"toDeg"`
	StepDeg  float64          `json:"stepDeg"`
	Points   []Point          `json:"points"`
	Warnings []string         `json:"warnings,omitempty"`
}

// Sweep 在 [fromDeg, toDeg] 区间内按 stepDeg 步长逐点计算 GZ 与复原力矩。
// 区间两端点一定包含在结果中；若步长不能整除区间，最后一个区间自动缩短。
func Sweep(in stability.Input, fromDeg, toDeg, stepDeg float64) (Curve, error) {
	if err := stability.Validate(in); err != nil {
		return Curve{}, err
	}
	var problems []string
	for name, v := range map[string]float64{"fromDeg": fromDeg, "toDeg": toDeg, "stepDeg": stepDeg} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			problems = append(problems, fmt.Sprintf("扫描参数 %s 必须为有限数值，当前值 %v", name, v))
		}
	}
	if len(problems) == 0 {
		if stepDeg <= 0 {
			problems = append(problems, fmt.Sprintf("扫描步长 stepDeg 必须为正，当前值 %v°", stepDeg))
		}
		if toDeg <= fromDeg {
			problems = append(problems, fmt.Sprintf("扫描终点 toDeg 必须大于起点 fromDeg，当前 [%v°, %v°]", fromDeg, toDeg))
		}
	}
	if len(problems) > 0 {
		return Curve{}, &stability.ValidationError{Problems: problems}
	}

	angles, err := grid(fromDeg, toDeg, stepDeg)
	if err != nil {
		return Curve{}, err
	}

	curve := Curve{FromDeg: fromDeg, ToDeg: toDeg, StepDeg: stepDeg}
	beyond := false
	for i, a := range angles {
		res, err := stability.EvaluateDeg(in, a)
		if err != nil { // 参数已校验，理论上不会触发
			return Curve{}, fmt.Errorf("扫描第 %d 点（%.6f°）失败: %w", i, a, err)
		}
		if i == 0 {
			curve.BM, curve.GM = res.BM, res.GM
			curve.Stable, curve.Status = res.Stable, res.Status
			curve.Warnings = append(curve.Warnings, res.Warnings...)
		}
		if res.BeyondSmallAngle {
			beyond = true
		}
		curve.Points = append(curve.Points, Point{
			AngleDeg:         a,
			AngleRad:         res.HeelAngleRad,
			GZ:               res.GZ,
			RightingMoment:   res.RightingMoment,
			BeyondSmallAngle: res.BeyondSmallAngle,
		})
	}
	if beyond {
		curve.Warnings = append(curve.Warnings,
			fmt.Sprintf("扫描区间包含超出小倾角近似适用范围（±%.0f°）的角度，相应点为外推结果，仅供参考",
				stability.SmallAngleLimitDeg))
	}
	return curve, nil
}

// grid 生成扫描角度序列：起点、各步长点，并保证终点精确落在 toDeg。
func grid(fromDeg, toDeg, stepDeg float64) ([]float64, error) {
	eps := 1e-9 * math.Max(1, math.Abs(toDeg))
	angles := []float64{fromDeg}
	for a := fromDeg + stepDeg; a < toDeg-eps; a += stepDeg {
		angles = append(angles, a)
		if len(angles) > MaxPoints {
			return nil, &stability.ValidationError{Problems: []string{
				fmt.Sprintf("扫描点数超过上限 %d，请增大步长或缩小区间", MaxPoints)}}
		}
	}
	if last := angles[len(angles)-1]; toDeg-last > eps {
		angles = append(angles, toDeg)
	} else {
		angles[len(angles)-1] = toDeg
	}
	return angles, nil
}
