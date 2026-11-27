# `State.SourceKnown`: `on_ac` false with `source_known` false means *unknown*, never battery

**`State.SourceKnown`: `on_ac` false with `source_known` false means
*unknown*, never battery.** `linux.OnACPower` errors when no Mains supply
exists (VM, desktop), and get-state used to flatten that to `on_ac: false` —
indistinguishable from running on battery. Clients must claim nothing when
it is false (`profileui.PowerLabel` returns ""), and a pre-2.0 daemon omits
the field, so old daemons read as unknown by construction.
