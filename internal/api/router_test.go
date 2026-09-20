package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"stability-service/internal/loadcase"
	"stability-service/internal/stability"
)

func newTestRouter(t *testing.T) (*gin.Engine, *loadcase.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := loadcase.Open(filepath.Join(t.TempDir(), "loadcases.json"))
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	return NewRouter(store), store
}

func doJSON(t *testing.T, r *gin.Engine, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("序列化请求体失败: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var decoded map[string]any
	if len(w.Body.Bytes()) > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("响应不是合法 JSON: %v, body=%s", err, w.Body.String())
		}
	}
	return w, decoded
}

func validEvalBody() map[string]any {
	return map[string]any{
		"volume":       1000.0,
		"kb":           2.0,
		"kg":           3.0,
		"it":           2000.0,
		"heelAngleDeg": 10.0,
	}
}

func TestEvaluatePositiveStability(t *testing.T) {
	r, _ := newTestRouter(t)
	w, body := doJSON(t, r, http.MethodPost, "/api/v1/stability/evaluate", validEvalBody())
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，body = %v", w.Code, body)
	}
	if body["bm"] != 2.0 || body["gm"] != 1.0 {
		t.Errorf("BM/GM = %v/%v，期望 2/1", body["bm"], body["gm"])
	}
	if body["stable"] != true || body["status"] != string(stability.StatusPositive) {
		t.Errorf("应判定为正稳性，实际 stable=%v status=%v", body["stable"], body["status"])
	}
	wantGZ := 1.0 * math.Sin(math.Pi/18)
	if got := body["gz"].(float64); math.Abs(got-wantGZ) > 1e-12 {
		t.Errorf("GZ = %v，期望 GM·sin(10°) = %v", got, wantGZ)
	}
	wantM := stability.DefaultDensity * 1000 * stability.DefaultGravity * wantGZ
	if got := body["rightingMoment"].(float64); math.Abs(got-wantM) > 1e-6*wantM {
		t.Errorf("复原力矩 = %v，期望 ρ∇g·GZ = %v", got, wantM)
	}
	if body["heelAngleRad"].(float64) != math.Pi/18 {
		t.Errorf("heelAngleRad = %v，期望 π/18", body["heelAngleRad"])
	}
}

func TestEvaluateNegativeStability(t *testing.T) {
	r, _ := newTestRouter(t)
	reqBody := validEvalBody()
	reqBody["kg"] = 9.0 // GM = 2 + 2 − 9 = −5
	w, body := doJSON(t, r, http.MethodPost, "/api/v1/stability/evaluate", reqBody)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，body = %v", w.Code, body)
	}
	if body["gm"] != -5.0 {
		t.Errorf("GM = %v，期望 −5", body["gm"])
	}
	if body["stable"] != false || body["status"] != string(stability.StatusNegative) {
		t.Errorf("应判定为负稳性，实际 stable=%v status=%v", body["stable"], body["status"])
	}
	if body["gz"].(float64) >= 0 {
		t.Errorf("负稳性下 10° 横倾的 GZ 应为负，实际 %v", body["gz"])
	}
}

// 角度单位换算：度入口与弧度入口结果一致；10° 的 GZ 必须为正
// （若漏掉度→弧度换算，sin(10 rad) < 0，结果会变号）。
func TestEvaluateAngleUnitConversion(t *testing.T) {
	r, _ := newTestRouter(t)

	_, byDeg := doJSON(t, r, http.MethodPost, "/api/v1/stability/evaluate", validEvalBody())
	if byDeg["gz"].(float64) <= 0 {
		t.Fatalf("正稳性船 10° 横倾的 GZ 应为正，实际 %v（疑似角度未换算为弧度）", byDeg["gz"])
	}

	radBody := validEvalBody()
	delete(radBody, "heelAngleDeg")
	radBody["heelAngleRad"] = math.Pi / 18
	_, byRad := doJSON(t, r, http.MethodPost, "/api/v1/stability/evaluate", radBody)
	if byDeg["gz"] != byRad["gz"] || byDeg["rightingMoment"] != byRad["rightingMoment"] {
		t.Errorf("度/弧度入口结果不一致：GZ %v vs %v", byDeg["gz"], byRad["gz"])
	}
}

func TestEvaluateRejectsInvalidParams(t *testing.T) {
	r, _ := newTestRouter(t)
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"排水体积为零", func(b map[string]any) { b["volume"] = 0 }},
		{"排水体积为负", func(b map[string]any) { b["volume"] = -1 }},
		{"惯性矩为负", func(b map[string]any) { b["it"] = -2 }},
		{"重心高度为负", func(b map[string]any) { b["kg"] = -0.5 }},
		{"缺少横倾角", func(b map[string]any) { delete(b, "heelAngleDeg") }},
		{"两种角度单位同时给出", func(b map[string]any) { b["heelAngleRad"] = 0.1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validEvalBody()
			tc.mutate(body)
			w, resp := doJSON(t, r, http.MethodPost, "/api/v1/stability/evaluate", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d，期望 400，body = %v", w.Code, resp)
			}
			if resp["error"] == nil || resp["error"] == "" {
				t.Errorf("错误响应应说明原因，实际 %v", resp)
			}
		})
	}
}

func TestCurveEndpoint(t *testing.T) {
	r, _ := newTestRouter(t)
	w, body := doJSON(t, r, http.MethodPost, "/api/v1/stability/curve", map[string]any{
		"volume": 1000.0, "kb": 2.0, "kg": 3.0, "it": 2000.0,
		"fromDeg": 0, "toDeg": 10, "stepDeg": 1,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，body = %v", w.Code, body)
	}
	points, ok := body["points"].([]any)
	if !ok || len(points) != 11 {
		t.Fatalf("0..10° 步长 1° 应有 11 个点，实际 %v", body["points"])
	}
	first := points[0].(map[string]any)
	if first["angleDeg"] != 0.0 || first["gz"] != 0.0 {
		t.Errorf("首点应为 (0°, GZ=0)，实际 %v", first)
	}
	prev := -1.0
	for i, p := range points {
		pt := p.(map[string]any)
		gz := pt["gz"].(float64)
		if gz <= prev {
			t.Errorf("正稳性下 GZ 应随角度单调上升，第 %d 点 %v 不大于前点 %v", i, gz, prev)
		}
		prev = gz
		if math.Abs(pt["angleRad"].(float64)-stability.DegToRad(pt["angleDeg"].(float64))) > 1e-15 {
			t.Errorf("第 %d 点弧度值与角度值不对应", i)
		}
	}
	if body["gm"] != 1.0 || body["stable"] != true {
		t.Errorf("曲线级 GM/stable = %v/%v，期望 1/true", body["gm"], body["stable"])
	}
}

func TestCurveEndpointValidation(t *testing.T) {
	r, _ := newTestRouter(t)
	w, body := doJSON(t, r, http.MethodPost, "/api/v1/stability/curve", map[string]any{
		"volume": 1000.0, "kb": 2.0, "kg": 3.0, "it": 2000.0,
		"fromDeg": 0, "toDeg": 10, "stepDeg": 0,
	})
	if w.Code != http.StatusBadRequest || body["error"] == nil {
		t.Errorf("步长为零应返回 400 并说明原因，实际 code=%d body=%v", w.Code, body)
	}
}

func validCaseBody(name string) map[string]any {
	return map[string]any{
		"name": name, "volume": 1000.0, "kb": 2.0, "kg": 3.0, "it": 2000.0,
	}
}

func TestLoadcaseCRUD(t *testing.T) {
	r, _ := newTestRouter(t)

	w, _ := doJSON(t, r, http.MethodPost, "/api/v1/loadcases", validCaseBody("loaded"))
	if w.Code != http.StatusCreated {
		t.Fatalf("创建状态码 = %d", w.Code)
	}
	w, body := doJSON(t, r, http.MethodPost, "/api/v1/loadcases", validCaseBody("loaded"))
	if w.Code != http.StatusConflict || body["error"] == nil {
		t.Errorf("同名创建应返回 409，实际 code=%d body=%v", w.Code, body)
	}

	w, body = doJSON(t, r, http.MethodGet, "/api/v1/loadcases/loaded", nil)
	if w.Code != http.StatusOK || body["name"] != "loaded" {
		t.Errorf("取回失败: code=%d body=%v", w.Code, body)
	}

	w, body = doJSON(t, r, http.MethodGet, "/api/v1/loadcases", nil)
	if w.Code != http.StatusOK || len(body["cases"].([]any)) != 1 {
		t.Errorf("列表应含 1 份档，实际 %v", body)
	}

	upd := validCaseBody("loaded")
	upd["kg"] = 4.0
	w, _ = doJSON(t, r, http.MethodPut, "/api/v1/loadcases/loaded", upd)
	if w.Code != http.StatusOK {
		t.Fatalf("更新状态码 = %d", w.Code)
	}
	_, body = doJSON(t, r, http.MethodGet, "/api/v1/loadcases/loaded", nil)
	if body["input"].(map[string]any)["kg"] != 4.0 {
		t.Errorf("更新后 KG 应为 4，实际 %v", body["input"])
	}

	w, _ = doJSON(t, r, http.MethodDelete, "/api/v1/loadcases/loaded", nil)
	if w.Code != http.StatusNoContent {
		t.Errorf("删除状态码 = %d，期望 204", w.Code)
	}
	w, body = doJSON(t, r, http.MethodGet, "/api/v1/loadcases/loaded", nil)
	if w.Code != http.StatusNotFound || body["error"] == nil {
		t.Errorf("删除后取回应 404，实际 code=%d body=%v", w.Code, body)
	}
}

func TestLoadcaseEvaluateAndCurve(t *testing.T) {
	r, _ := newTestRouter(t)
	if w, _ := doJSON(t, r, http.MethodPost, "/api/v1/loadcases", validCaseBody("ballast")); w.Code != http.StatusCreated {
		t.Fatal("创建装载状态失败")
	}

	w, body := doJSON(t, r, http.MethodPost, "/api/v1/loadcases/ballast/evaluate", map[string]any{"heelAngleDeg": 10})
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，body = %v", w.Code, body)
	}
	res := body["result"].(map[string]any)
	if res["gm"] != 1.0 || res["stable"] != true {
		t.Errorf("按档重算 GM/stable = %v/%v，期望 1/true", res["gm"], res["stable"])
	}

	w, body = doJSON(t, r, http.MethodPost, "/api/v1/loadcases/ballast/curve", map[string]any{
		"fromDeg": 0, "toDeg": 5, "stepDeg": 1,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，body = %v", w.Code, body)
	}
	curve := body["curve"].(map[string]any)
	if len(curve["points"].([]any)) != 6 {
		t.Errorf("0..5° 步长 1° 应有 6 个点，实际 %v", curve["points"])
	}

	w, body = doJSON(t, r, http.MethodPost, "/api/v1/loadcases/ghost/evaluate", map[string]any{"heelAngleDeg": 10})
	if w.Code != http.StatusNotFound || body["error"] == nil {
		t.Errorf("不存在的档应返回 404，实际 code=%d body=%v", w.Code, body)
	}
}

// 预置矩形驳船算例：IT = B³L/12 手工可核对，GM 解出为正。
func TestSeededBargeMatchesHandCalculation(t *testing.T) {
	r, store := newTestRouter(t)
	if err := loadcase.SeedDemoBarge(store); err != nil {
		t.Fatalf("预置示范算例失败: %v", err)
	}

	w, body := doJSON(t, r, http.MethodGet, "/api/v1/loadcases/"+loadcase.DemoBargeName, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("取回示范算例失败: code=%d body=%v", w.Code, body)
	}
	input := body["input"].(map[string]any)
	// 手工核算：L=40 B=10 T=2 → ∇=800 m³，IT=10³·40/12=3333.33 m⁴。
	if input["volume"] != 800.0 {
		t.Errorf("示范驳船排水体积 = %v，期望 800", input["volume"])
	}
	if got := input["it"].(float64); math.Abs(got-10000.0/3.0) > 1e-9 {
		t.Errorf("示范驳船 IT = %v，期望 B³L/12 = 3333.333", got)
	}

	w, body = doJSON(t, r, http.MethodPost, "/api/v1/loadcases/"+loadcase.DemoBargeName+"/evaluate",
		map[string]any{"heelAngleDeg": 10})
	if w.Code != http.StatusOK {
		t.Fatalf("示范算例核算失败: code=%d body=%v", w.Code, body)
	}
	res := body["result"].(map[string]any)
	// BM = 3333.33/800 = 4.1667 m，GM = 1 + 4.1667 − 3 = 2.1667 m > 0。
	if got := res["bm"].(float64); math.Abs(got-25.0/6.0) > 1e-9 {
		t.Errorf("示范驳船 BM = %v，期望 4.1667", got)
	}
	if got := res["gm"].(float64); math.Abs(got-13.0/6.0) > 1e-9 {
		t.Errorf("示范驳船 GM = %v，期望 2.1667", got)
	}
	if res["stable"] != true || res["status"] != string(stability.StatusPositive) {
		t.Errorf("示范驳船必须解出正稳性，实际 stable=%v status=%v", res["stable"], res["status"])
	}
}

// 同名与不同名的装载状态并行核算，结果互不渗透。
func TestConcurrentLoadcaseIsolation(t *testing.T) {
	r, store := newTestRouter(t)

	mk := func(name string, kg float64) loadcase.Case {
		return loadcase.Case{
			Name:  name,
			Input: stability.Input{Volume: 1000, KB: 2, KG: kg, IT: 2000},
		}
	}
	// 两条"船"：KG 不同 → GM 不同（1.0 与 0.5）。
	if err := store.Create(mk("ship-a", 3.0)); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if err := store.Create(mk("ship-b", 3.5)); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	wantGM := map[string]float64{"ship-a": 1.0, "ship-b": 0.5}

	const goroutines = 64
	var wg sync.WaitGroup
	errs := make(chan string, goroutines*4)

	for i := 0; i < goroutines; i++ {
		for _, name := range []string{"ship-a", "ship-b"} {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost,
					"/api/v1/loadcases/"+name+"/evaluate",
					bytes.NewReader([]byte(`{"heelAngleDeg":10}`)))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					errs <- fmt.Sprintf("%s: 状态码 %d", name, w.Code)
					return
				}
				var resp struct {
					Loadcase string           `json:"loadcase"`
					Result   stability.Result `json:"result"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					errs <- fmt.Sprintf("%s: 响应解析失败 %v", name, err)
					return
				}
				if resp.Loadcase != name {
					errs <- fmt.Sprintf("请求 %s 的结果署名 %s", name, resp.Loadcase)
					return
				}
				if math.Abs(resp.Result.GM-wantGM[name]) > 1e-12 {
					errs <- fmt.Sprintf("%s: GM = %v，期望 %v（数据串档）", name, resp.Result.GM, wantGM[name])
				}
			}(name)
		}
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Error(msg)
	}
}
