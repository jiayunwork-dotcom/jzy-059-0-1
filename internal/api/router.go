// Package api 提供稳性核算服务的 HTTP 路由（Gin）。
package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"stability-service/internal/loadcase"
	"stability-service/internal/scan"
	"stability-service/internal/stability"
)

// Server 持有路由处理所需的依赖。
type Server struct {
	store *loadcase.Store
}

// NewRouter 构建服务的 HTTP 路由。
func NewRouter(store *loadcase.Store) *gin.Engine {
	s := &Server{store: store}
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	v1 := r.Group("/api/v1")
	v1.POST("/stability/evaluate", s.evaluate)
	v1.POST("/stability/curve", s.curve)
	v1.POST("/loadcases", s.createCase)
	v1.GET("/loadcases", s.listCases)
	v1.GET("/loadcases/:name", s.getCase)
	v1.PUT("/loadcases/:name", s.updateCase)
	v1.DELETE("/loadcases/:name", s.deleteCase)
	v1.POST("/loadcases/:name/evaluate", s.evaluateCase)
	v1.POST("/loadcases/:name/curve", s.curveCase)
	return r
}

// ---------- 请求 / 响应模型 ----------

// floatInput 是浮态几何参数的 JSON 表示。
type floatInput struct {
	Volume  float64 `json:"volume"`
	KB      float64 `json:"kb"`
	KG      float64 `json:"kg"`
	IT      float64 `json:"it"`
	Density float64 `json:"density"`
	Gravity float64 `json:"gravity"`
}

func (f floatInput) input() stability.Input {
	return stability.Input{
		Volume: f.Volume, KB: f.KB, KG: f.KG, IT: f.IT,
		Density: f.Density, Gravity: f.Gravity,
	}
}

// heelSpec 描述横倾角，度与弧度二选一。
type heelSpec struct {
	HeelAngleDeg *float64 `json:"heelAngleDeg"`
	HeelAngleRad *float64 `json:"heelAngleRad"`
}

// radians 把横倾角统一换算为弧度；缺省或重复提供时返回 false 并写出 400 响应。
func (h heelSpec) radians(c *gin.Context) (float64, bool) {
	switch {
	case h.HeelAngleDeg == nil && h.HeelAngleRad == nil:
		fail(c, http.StatusBadRequest, "缺少横倾角", "请提供 heelAngleDeg（度）或 heelAngleRad（弧度）之一")
		return 0, false
	case h.HeelAngleDeg != nil && h.HeelAngleRad != nil:
		fail(c, http.StatusBadRequest, "横倾角单位冲突", "heelAngleDeg 与 heelAngleRad 只能提供一个")
		return 0, false
	case h.HeelAngleDeg != nil:
		return stability.DegToRad(*h.HeelAngleDeg), true
	default:
		return *h.HeelAngleRad, true
	}
}

type evaluateRequest struct {
	floatInput
	heelSpec
}

type curveRequest struct {
	floatInput
	FromDeg float64 `json:"fromDeg"`
	ToDeg   float64 `json:"toDeg"`
	StepDeg float64 `json:"stepDeg"`
}

type caseRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	floatInput
	Particulars map[string]float64 `json:"particulars"`
}

func (r caseRequest) toCase(name string) loadcase.Case {
	return loadcase.Case{
		Name:        name,
		Description: r.Description,
		Input:       r.input(),
		Particulars: r.Particulars,
	}
}

// ---------- 稳性核算（无状态） ----------

func (s *Server) evaluate(c *gin.Context) {
	var req evaluateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体不是合法的 JSON", err.Error())
		return
	}
	heelRad, ok := req.heelSpec.radians(c)
	if !ok {
		return
	}
	res, err := stability.Evaluate(req.input(), heelRad)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (s *Server) curve(c *gin.Context) {
	var req curveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体不是合法的 JSON", err.Error())
		return
	}
	curve, err := scan.Sweep(req.input(), req.FromDeg, req.ToDeg, req.StepDeg)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, curve)
}

// ---------- 装载状态档管理 ----------

func (s *Server) createCase(c *gin.Context) {
	var req caseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体不是合法的 JSON", err.Error())
		return
	}
	kase := req.toCase(req.Name)
	if err := s.store.Create(kase); err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, kase)
}

func (s *Server) listCases(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"cases": s.store.List()})
}

func (s *Server) getCase(c *gin.Context) {
	kase, ok := s.store.Get(c.Param("name"))
	if !ok {
		fail(c, http.StatusNotFound, "装载状态不存在", c.Param("name"))
		return
	}
	c.JSON(http.StatusOK, kase)
}

func (s *Server) updateCase(c *gin.Context) {
	name := c.Param("name")
	var req caseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体不是合法的 JSON", err.Error())
		return
	}
	if req.Name != "" && req.Name != name {
		fail(c, http.StatusBadRequest, "请求体中的 name 与路径不一致", req.Name+" != "+name)
		return
	}
	kase := req.toCase(name)
	if err := s.store.Update(kase); err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, kase)
}

func (s *Server) deleteCase(c *gin.Context) {
	if err := s.store.Delete(c.Param("name")); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- 装载状态档核算 ----------

func (s *Server) evaluateCase(c *gin.Context) {
	kase, ok := s.store.Get(c.Param("name"))
	if !ok {
		fail(c, http.StatusNotFound, "装载状态不存在", c.Param("name"))
		return
	}
	var spec heelSpec
	if err := c.ShouldBindJSON(&spec); err != nil {
		fail(c, http.StatusBadRequest, "请求体不是合法的 JSON", err.Error())
		return
	}
	heelRad, ok := spec.radians(c)
	if !ok {
		return
	}
	res, err := stability.Evaluate(kase.Input, heelRad)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"loadcase": kase.Name, "result": res})
}

func (s *Server) curveCase(c *gin.Context) {
	kase, ok := s.store.Get(c.Param("name"))
	if !ok {
		fail(c, http.StatusNotFound, "装载状态不存在", c.Param("name"))
		return
	}
	var req curveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "请求体不是合法的 JSON", err.Error())
		return
	}
	curve, err := scan.Sweep(kase.Input, req.FromDeg, req.ToDeg, req.StepDeg)
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"loadcase": kase.Name, "curve": curve})
}

// ---------- 错误响应 ----------

func fail(c *gin.Context, code int, msg string, details ...string) {
	c.AbortWithStatusJSON(code, gin.H{"error": msg, "details": details})
}

// failErr 按错误类型映射 HTTP 状态码：参数校验 → 400，未找到 → 404，重名 → 409。
func failErr(c *gin.Context, err error) {
	var verr *stability.ValidationError
	switch {
	case errors.As(err, &verr):
		fail(c, http.StatusBadRequest, "参数校验失败", verr.Problems...)
	case errors.Is(err, loadcase.ErrNotFound):
		fail(c, http.StatusNotFound, "装载状态不存在", err.Error())
	case errors.Is(err, loadcase.ErrExists):
		fail(c, http.StatusConflict, "同名装载状态已存在", err.Error())
	default:
		fail(c, http.StatusInternalServerError, "内部错误", err.Error())
	}
}
