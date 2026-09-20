# 小倾角初稳性核算服务

船舶总布置阶段的初稳性核算后端：接收浮态几何参数，返回稳性指标与随横倾角变化的复原力臂（GZ）曲线。纯 HTTP 接口，无前端页面。

## 计算模型

已知排水体积 ∇、浮心高度 KB、重心高度 KG、水线面横向惯性矩 IT：

```
BM = IT / ∇                横稳心半径（m）
GM = KB + BM − KG          初稳性高度（m）：GM > 0 正稳性，GM < 0 初稳性丧失
GZ = GM · sinφ             复原力臂（m），φ 为横倾角
M  = ρ · ∇ · g · GZ        复原力矩（N·m），排水质量取 ρ∇
```

- **角度单位**：接口接受度（`heelAngleDeg`）或弧度（`heelAngleRad`）；以度计的角度一律先经 `DegToRad` 换算成弧度再进入三角函数。结果中同时返回两种单位，便于核对。
- **小倾角限制**：近似在 ±10° 内成立。超出后仍按公式给值，但结果带 `beyondSmallAngle: true` 与中文越界提醒。
- **默认值**：密度缺省 1025 kg/m³（海水），重力加速度缺省 9.81 m/s²；密度只影响力矩的牛顿数值，不影响以米计的 GM/GZ。

## 构建与运行

### Docker（测试随构建一并运行）

```bash
docker build -t stability-service .
docker run -p 8080:8080 -v stability-data:/data stability-service
```

### 本地（Go 1.22）

```bash
go test ./...        # 运行全部测试
go run ./cmd/server  # 启动服务，监听 :8080
```

环境变量：`STABILITY_ADDR`（监听地址，默认 `:8080`）、`STABILITY_DATA_DIR`（装载状态持久化目录，容器内默认 `/data`，本地默认 `./data`）。

## API 一览

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/healthz` | 健康检查 |
| POST | `/api/v1/stability/evaluate` | 单次核算：BM、GM、GZ、复原力矩、稳性正负 |
| POST | `/api/v1/stability/curve` | 横倾扫描：GZ 随角度变化的点列 |
| POST | `/api/v1/loadcases` | 装载状态建档（同名返回 409） |
| GET | `/api/v1/loadcases` | 列出全部装载状态 |
| GET | `/api/v1/loadcases/:name` | 按名取回 |
| PUT | `/api/v1/loadcases/:name` | 更新 |
| DELETE | `/api/v1/loadcases/:name` | 删除 |
| POST | `/api/v1/loadcases/:name/evaluate` | 按档重算 |
| POST | `/api/v1/loadcases/:name/curve` | 按档扫描 |

### 单次核算

```bash
curl -s localhost:8080/api/v1/stability/evaluate -d '{
  "volume": 800, "kb": 1.0, "kg": 3.0, "it": 3333.3333,
  "heelAngleDeg": 10
}'
```

```json
{
  "input": {"volume": 800, "kb": 1, "kg": 3, "it": 3333.3333, "density": 1025, "gravity": 9.81},
  "bm": 4.1667, "gm": 2.1667,
  "gz": 0.3762, "rightingMoment": 3027300,
  "heelAngleDeg": 10, "heelAngleRad": 0.17453,
  "stable": true, "status": "positive", "beyondSmallAngle": false
}
```

### 横倾扫描

```bash
curl -s localhost:8080/api/v1/stability/curve -d '{
  "volume": 800, "kb": 1.0, "kg": 3.0, "it": 3333.3333,
  "fromDeg": 0, "toDeg": 10, "stepDeg": 1
}'
```

返回 `points` 数组，每点含 `angleDeg / angleRad / gz / rightingMoment / beyondSmallAngle`，全部由内核公式逐点实算。

### 参数校验

排水体积或惯性矩不为正、竖向高度为负、角度单位冲突等，一律返回 `400` 并说明原因：

```json
{"error": "参数校验失败", "details": ["排水体积 ∇ (volume) 必须为正，当前值 -5 m³"]}
```

## 预置算例：矩形驳船 `demo-barge`

服务启动时自动建档（已存在则不覆盖），可直接核对：

```bash
curl -s localhost:8080/api/v1/loadcases/demo-barge/evaluate -d '{"heelAngleDeg": 10}'
```

手工核算（L=40 m，B=10 m，T=2 m，KG=3.0 m）：

```
∇  = L·B·T     = 800 m³
KB = T/2       = 1.0 m
IT = B³·L/12   = 10³·40/12 = 3333.33 m⁴
BM = IT/∇      = 4.1667 m
GM = KB+BM−KG  = 2.1667 m > 0   → 正稳性
GZ(10°)        = 2.1667·sin(10°) = 0.3762 m
```

## 代码结构

```
cmd/server/            服务入口
internal/stability/    稳性内核：BM/GM/GZ/力矩、度↔弧度换算、参数校验
internal/scan/         横倾角扫描，生成 GZ 曲线
internal/loadcase/     装载状态档管理 + JSON 持久化 + 预置驳船算例
internal/api/          HTTP 路由（Gin）
```

## 测试覆盖

- 缩放不变量：IT 加倍 → BM 与 GM 增量相同；KG 抬高 → GM 线性下降，越过 KB+BM 转负；驳船船宽加倍 → IT 变 8 倍、BM 大幅抬升；密度变化只影响力矩不影响 GM
- 零横倾 → GZ 恒为零
- 角度单位换算（度入口与弧度入口结果一致，10° 不会算成 10 rad）
- 正/负/临界稳性判定、小倾角越界提醒
- 非法参数拦截（体积/惯性矩非正、负高度、NaN、单位冲突）
- 装载状态 CRUD、持久化重开、同名/不同名并发隔离（`go test -race` 通过）
