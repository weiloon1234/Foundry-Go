package imaging

// Invert reverses RGB channels, preserving alpha.
func (p Plan) Invert() Plan { return p.append(step{kind: invert}) }

// Gamma applies gamma correction. Values above one lighten; below one darken.
func (p Plan) Gamma(value float64) Plan { return p.append(step{kind: gamma, number: value}) }

// Saturation changes saturation by a percentage in [-100,100].
func (p Plan) Saturation(percentage float64) Plan {
	return p.append(step{kind: saturation, number: percentage})
}

// Hue shifts hue by degrees in [-360,360].
func (p Plan) Hue(degrees float64) Plan { return p.append(step{kind: hue, number: degrees}) }

// Sepia applies a sepia effect with strength in [0,100].
func (p Plan) Sepia(percentage float64) Plan {
	return p.append(step{kind: sepia, number: percentage})
}

// Threshold makes RGB black or white using a luminance percentage in [0,100].
func (p Plan) Threshold(percentage float64) Plan {
	return p.append(step{kind: threshold, number: percentage})
}

// Pixelate replaces square blocks with their average color.
func (p Plan) Pixelate(size int) Plan { return p.append(step{kind: pixelate, width: size}) }

// Sharpen applies an unsharp mask. Sigma is in (0,100], amount in [0,10],
// threshold in [0,1]. Typical values are (1, 1, 0.02).
func (p Plan) Sharpen(sigma, amount, threshold float64) Plan {
	return p.append(step{kind: sharpen, number: sigma, amount: amount, threshold: threshold})
}
