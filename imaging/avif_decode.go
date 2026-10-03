package imaging

import (
	"bytes"
	"image"

	"github.com/gen2brain/gav1d/avif"
)

func decodeAVIF(data []byte, container *avifContainer) (image.Image, error) {
	if container.track != nil {
		sequence, err := decodeAVIFSequence(data, container)
		if err != nil {
			return nil, err
		}
		return sequence.frames[0], nil
	}
	return avif.Decode(bytes.NewReader(data), avif.Options{FrameSizeLimit: int(container.maxFramePixels)})
}

func decodeAVIFSequence(data []byte, container *avifContainer) (imageSequence, error) {
	if container == nil || container.track == nil {
		return imageSequence{}, unsupported()
	}
	track := container.track
	plays, err := track.plays()
	if err != nil {
		return imageSequence{}, err
	}
	decoded, err := avif.DecodeAll(bytes.NewReader(data), avif.Options{FrameSizeLimit: int(container.maxFramePixels)})
	if err != nil {
		return imageSequence{}, invalid("invalid AVIF animation")
	}
	// DecodeAll falls back to the primary still image on a sequence error.
	// Require the exact inspected timeline, including positive delays for a
	// one-frame sequence, so that fallback never becomes successful processing.
	if len(decoded.Image) != len(track.samples) || len(decoded.Delay) != len(track.samples) {
		return imageSequence{}, invalid("decoded AVIF sequence differs from its header")
	}
	decodedPlays := uint32(0)
	if decoded.LoopCount < 0 {
		decodedPlays = 1
	} else if decoded.LoopCount > 0 {
		decodedPlays = uint32(decoded.LoopCount) + 1
	}
	if decodedPlays != plays {
		return imageSequence{}, invalid("decoded AVIF loop count differs from its header")
	}
	sequence := imageSequence{frames: decoded.Image, plays: plays}
	for i, img := range decoded.Image {
		if img == nil || img.Bounds().Dx() != track.width || img.Bounds().Dy() != track.height || decoded.Delay[i] != float64(track.deltas[i])/float64(track.timescale) {
			return imageSequence{}, invalid("decoded AVIF frame differs from its header")
		}
		sequence.delays = append(sequence.delays, frameDelay{track.deltas[i], track.timescale})
	}
	return sequence, nil
}
