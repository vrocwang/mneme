package store

import "testing"

func TestEncodeDecodeVector(t *testing.T) {
	original := []float32{0.1, -0.2, 3.14, 0.0}
	encoded := EncodeVector(original)
	decoded, err := DecodeVector(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(original) {
		t.Fatalf("expected %d elements, got %d", len(original), len(decoded))
	}
	for i := range original {
		if abs(float64(decoded[i]-original[i])) > 1e-6 {
			t.Errorf("element %d: expected %f, got %f", i, original[i], decoded[i])
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	if CosineSimilarity(a, b) != 1.0 {
		t.Errorf("identical vectors should have similarity 1.0, got %f", CosineSimilarity(a, b))
	}

	c := []float32{0, 1, 0}
	sim := CosineSimilarity(a, c)
	if sim != 0.0 {
		t.Errorf("orthogonal vectors should have similarity 0.0, got %f", sim)
	}

	d := []float32{-1, 0, 0}
	sim2 := CosineSimilarity(a, d)
	// Negative cosine values are clamped to 0 for semantic search.
	if sim2 != 0.0 {
		t.Errorf("opposite vectors should have similarity 0.0 (clamped), got %f", sim2)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	if CosineSimilarity([]float32{1, 0}, []float32{1}) != 0 {
		t.Error("different length vectors should return 0")
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
