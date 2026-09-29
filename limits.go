package appmeta

// Limits bound the work Parse does on one input. The defaults fit real-world
// apps with a wide margin; lower them for tighter memory or latency budgets.
type Limits struct {
	// MaxEntries is the most zip entries the archive may declare. It is
	// checked before the central directory is read.
	MaxEntries int
	// MaxEntrySize is the most uncompressed bytes read from one zip entry.
	MaxEntrySize int64
	// MaxTotalSize is the most uncompressed bytes read across all entries.
	MaxTotalSize int64
	// MaxDepth is the deepest element nesting accepted in binary XML and plists.
	MaxDepth int
	// MaxIconPixels is the largest width*height of a source icon that will
	// be decoded.
	MaxIconPixels int
}

// MaxIconSize is the largest width and height of an extracted icon; larger
// icons are scaled down.
const MaxIconSize = 512

// DefaultLimits returns the limits Parse uses unless WithLimits overrides them.
func DefaultLimits() Limits {
	return Limits{
		MaxEntries:    200_000,
		MaxEntrySize:  64 << 20,
		MaxTotalSize:  256 << 20,
		MaxDepth:      64,
		MaxIconPixels: 4096 * 4096,
	}
}

// Option configures Parse.
type Option func(*config)

type config struct {
	limits Limits
}

// WithLimits overrides the default limits. Zero fields keep their default.
func WithLimits(l Limits) Option {
	return func(c *config) {
		if l.MaxEntries > 0 {
			c.limits.MaxEntries = l.MaxEntries
		}
		if l.MaxEntrySize > 0 {
			c.limits.MaxEntrySize = l.MaxEntrySize
		}
		if l.MaxTotalSize > 0 {
			c.limits.MaxTotalSize = l.MaxTotalSize
		}
		if l.MaxDepth > 0 {
			c.limits.MaxDepth = l.MaxDepth
		}
		if l.MaxIconPixels > 0 {
			c.limits.MaxIconPixels = l.MaxIconPixels
		}
	}
}
