package main

// Tomislav-RetCtx: a measured checkpoint fraction can accompany a conditional
// payload histogram. Separate random streams choose membership and value size;
// position-based draws preserve both across worker/batch assignment changes.
// This models the marginal span mix, not a trace's TTL or topology.
func (c config) checkpointAt(position uint64) (bool, uint64) {
	if c.CheckpointFraction == nil {
		return position%uint64(c.CPD) == 0, position / uint64(c.CPD)
	}
	x := mixSizeBits((c.Seed ^ 0xd1b54a32d192ed03) + (position+1)*0x9e3779b97f4a7c15)
	u := float64(x>>11) * (1.0 / (1 << 53))
	return u < *c.CheckpointFraction, position
}
