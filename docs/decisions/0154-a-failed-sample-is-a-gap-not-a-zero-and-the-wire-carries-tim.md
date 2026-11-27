# A failed sample is a gap, not a zero, and the wire carries timestamps so a client can see it

**A failed sample is a gap, not a zero, and the wire carries timestamps so a
client can see it.** `api.TelemetrySample.At` is Unix seconds and samples are
**not evenly spaced** — the sampler stands down across a suspend and skips a
failed read — so a client that plots against the array index draws a suspend
as if no time passed. Recording a zero instead would be worse than the gap: 0°C
reads as a measurement.
