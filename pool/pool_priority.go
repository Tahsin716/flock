package pool

type PriorityPool struct {
	high   *Pool
	normal *Pool
	low    *Pool
}

func NewPriorityPool(highCap, normalCap, lowCap int) *PriorityPool {
	return &PriorityPool{
		high:   New(highCap),
		normal: New(normalCap),
		low:    New(lowCap),
	}
}

func (pp *PriorityPool) GoHigh(task Task) error {
	return pp.high.Go(task)
}

func (pp *PriorityPool) Go(task Task) error {
	return pp.normal.Go(task)
}

func (pp *PriorityPool) GoLow(task Task) error {
	return pp.low.Go(task)
}

func (pp *PriorityPool) Wait() {
	pp.high.Wait()
	pp.normal.Wait()
	pp.low.Wait()
}

func (pp *PriorityPool) Close() error {
	pp.high.Close()
	pp.normal.Close()
	pp.low.Close()
	return nil
}
