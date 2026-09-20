package loadcase

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sync"
	"testing"

	"stability-service/internal/stability"
)

func validCase(name string) Case {
	return Case{
		Name:  name,
		Input: stability.Input{Volume: 1000, KB: 2, KG: 3, IT: 2000},
	}
}

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "loadcases.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	return s, path
}

func TestCreateGetListDelete(t *testing.T) {
	s, _ := openTemp(t)

	if err := s.Create(validCase("full-load")); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	c, ok := s.Get("full-load")
	if !ok {
		t.Fatal("创建后应能取回")
	}
	if c.Input.Volume != 1000 || c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		t.Errorf("取回的档内容不完整: %+v", c)
	}

	if err := s.Create(validCase("ballast")); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	list := s.List()
	if len(list) != 2 || list[0].Name != "ballast" || list[1].Name != "full-load" {
		t.Errorf("List 应按名字排序返回两份档，实际 %+v", list)
	}

	if err := s.Delete("full-load"); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, ok := s.Get("full-load"); ok {
		t.Error("删除后不应再取到")
	}
	if err := s.Delete("full-load"); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
}

func TestCreateDuplicateRejected(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.Create(validCase("a")); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if err := s.Create(validCase("a")); !errors.Is(err, ErrExists) {
		t.Errorf("同名创建应返回 ErrExists，实际 %v", err)
	}
}

func TestUpdateKeepsCreatedAt(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.Create(validCase("a")); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	created, _ := s.Get("a")

	upd := validCase("a")
	upd.Input.KG = 4.5
	if err := s.Update(upd); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	got, _ := s.Get("a")
	if got.Input.KG != 4.5 {
		t.Errorf("更新后 KG = %v，期望 4.5", got.Input.KG)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("更新不应改变 CreatedAt：%v -> %v", created.CreatedAt, got.CreatedAt)
	}

	if err := s.Update(validCase("ghost")); !errors.Is(err, ErrNotFound) {
		t.Errorf("更新不存在的档应返回 ErrNotFound，实际 %v", err)
	}
}

// 落盘后重新打开，数据必须原样取回。
func TestPersistenceAcrossReopen(t *testing.T) {
	s, path := openTemp(t)
	if err := s.Create(validCase("persisted")); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if err := s.Create(DemoBarge()); err != nil {
		t.Fatalf("Create 示范算例失败: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("重新 Open 失败: %v", err)
	}
	c, ok := s2.Get("persisted")
	if !ok {
		t.Fatal("重开后应能取回已保存的档")
	}
	if c.Input.Volume != 1000 || c.Input.IT != 2000 {
		t.Errorf("重开后档内容不符: %+v", c.Input)
	}
	if _, ok := s2.Get(DemoBargeName); !ok {
		t.Error("重开后示范驳船算例应仍在")
	}
}

// 非法浮态参数与非法名字都要被挡在存储之外。
func TestInvalidCaseRejected(t *testing.T) {
	s, _ := openTemp(t)

	bad := validCase("bad")
	bad.Input.Volume = 0
	if err := s.Create(bad); err == nil {
		t.Error("排水体积为零的档应被拒绝")
	}
	var verr *stability.ValidationError
	if err := s.Create(bad); !errors.As(err, &verr) {
		t.Errorf("非法浮态参数应返回 *stability.ValidationError，实际 %T", err)
	}

	for _, name := range []string{"", "a/b", "a\\b"} {
		if err := s.Create(validCase(name)); err == nil {
			t.Errorf("非法名字 %q 应被拒绝", name)
		}
	}
	if _, ok := s.Get("bad"); ok {
		t.Error("被拒绝的档不应留存在存储中")
	}
}

// 预置矩形驳船算例：IT 按 B³L/12 手工可核对，GM 解出为正。
func TestDemoBargeHandCheck(t *testing.T) {
	barge := DemoBarge()
	const L, B, T = 40.0, 10.0, 2.0
	if got := barge.Input.Volume; got != L*B*T {
		t.Errorf("排水体积 = %v，期望 %v", got, L*B*T)
	}
	wantIT := B * B * B * L / 12 // 3333.333… m⁴
	if got := barge.Input.IT; math.Abs(got-wantIT) > 1e-9*wantIT {
		t.Errorf("IT = %v，期望 B³L/12 = %v", got, wantIT)
	}
	res, err := stability.EvaluateDeg(barge.Input, 10)
	if err != nil {
		t.Fatalf("EvaluateDeg 失败: %v", err)
	}
	// 手工核算：BM = 3333.33/800 ≈ 4.1667 m，GM = 1 + 4.1667 − 3 = 2.1667 m。
	if math.Abs(res.BM-4.1666667) > 1e-4 {
		t.Errorf("BM = %v，手工核算期望 ≈4.1667", res.BM)
	}
	if math.Abs(res.GM-2.1666667) > 1e-4 {
		t.Errorf("GM = %v，手工核算期望 ≈2.1667", res.GM)
	}
	if !res.Stable {
		t.Error("示范驳船算例的 GM 必须解出为正")
	}
}

func TestSeedDemoBargeIdempotent(t *testing.T) {
	s, _ := openTemp(t)
	if err := SeedDemoBarge(s); err != nil {
		t.Fatalf("SeedDemoBarge 失败: %v", err)
	}
	first, _ := s.Get(DemoBargeName)
	if err := SeedDemoBarge(s); err != nil {
		t.Fatalf("重复 SeedDemoBarge 不应报错: %v", err)
	}
	second, _ := s.Get(DemoBargeName)
	if !first.CreatedAt.Equal(second.CreatedAt) {
		t.Error("重复预置不应覆盖既有算例")
	}
}

// 并发读写：同名与不同名的档并行操作，结果互不渗透。
func TestConcurrentIsolation(t *testing.T) {
	s, _ := openTemp(t)

	const workers = 32
	const rounds = 20

	// 预建两个 KG 不同的档，GM 一正一负，便于识别串数据。
	a := validCase("ship-a") // GM = +1
	b := validCase("ship-b")
	b.Input.KG = 9 // GM = -4
	if err := s.Create(a); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if err := s.Create(b); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, workers*rounds*2)

	// 一半 goroutine 反复读两个档并核对各自 KG；一半并发创建/更新/删除各自的档。
	for w := 0; w < workers; w++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				ca, ok := s.Get("ship-a")
				if !ok || ca.Input.KG != 3 {
					errs <- fmt.Errorf("ship-a 数据被污染: ok=%v KG=%v", ok, ca.Input.KG)
					return
				}
				cb, ok := s.Get("ship-b")
				if !ok || cb.Input.KG != 9 {
					errs <- fmt.Errorf("ship-b 数据被污染: ok=%v KG=%v", ok, cb.Input.KG)
					return
				}
			}
		}()
		go func(w int) {
			defer wg.Done()
			name := fmt.Sprintf("worker-%d", w)
			kase := validCase(name)
			kase.Input.KG = float64(w)
			for i := 0; i < rounds; i++ {
				if err := s.Create(kase); err != nil && !errors.Is(err, ErrExists) {
					errs <- fmt.Errorf("Create %s: %w", name, err)
					return
				}
				kase.Input.KG = float64(w) + float64(i)*0.001
				if err := s.Update(kase); err != nil {
					errs <- fmt.Errorf("Update %s: %w", name, err)
					return
				}
				got, ok := s.Get(name)
				if !ok || got.Input.KG != kase.Input.KG {
					errs <- fmt.Errorf("%s 读回数据与写入不符", name)
					return
				}
			}
			if err := s.Delete(name); err != nil {
				errs <- fmt.Errorf("Delete %s: %w", name, err)
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if len(s.List()) != 2 {
		t.Errorf("并发结束后应只剩两个预建档，实际 %d 个", len(s.List()))
	}
}
