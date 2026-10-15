package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStageBitForGrade(t *testing.T) {
	cases := []struct {
		grade int
		bit   int
	}{
		{1, StagePrimary}, {6, StagePrimary},
		{7, StageJunior}, {9, StageJunior},
		{10, StageSenior}, {12, StageSenior},
		{0, 0}, {13, 0}, {-1, 0},
	}
	for _, c := range cases {
		require.Equal(t, c.bit, StageBitForGrade(c.grade), "grade=%d", c.grade)
	}
}

func TestGradeMaskAllCoversEveryStage(t *testing.T) {
	for _, bit := range []int{StagePrimary, StageJunior, StageSenior} {
		require.NotZero(t, GradeMaskAll&bit, "全学段掩码应包含每一位")
	}
}

func TestTrackBitsDoNotCollideWithStageBits(t *testing.T) {
	// High 2 track bits must sit above the low 3 stage bits and never overlap.
	require.Equal(t, 8, TrackArts)
	require.Equal(t, 16, TrackScience)
	require.Equal(t, 0, GradeMaskAll&TrackMaskAll, "学段位与文理位不得重叠")
	require.Equal(t, 31, MaskAll, "全开掩码=31")
	require.Equal(t, 31, MaskMax)
}

func TestTrackBitFor(t *testing.T) {
	cases := []struct {
		track int
		bit   int
	}{
		{KidTrackArts, TrackArts},
		{KidTrackScience, TrackScience},
		{KidTrackUndecided, 0}, // undecided → no track filter
		{3, 0}, {-1, 0},
	}
	for _, c := range cases {
		require.Equal(t, c.bit, TrackBitFor(c.track), "track=%d", c.track)
	}
}
