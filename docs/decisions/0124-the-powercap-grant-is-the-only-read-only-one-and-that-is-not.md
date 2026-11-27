# The powercap grant is the only read-only one, and that is not an accident

**The powercap grant is the only read-only one, and that is not an
accident.** Every other target `voltaire setup` touches is `chmod g+w`
because voltaire writes it. `/sys/class/powercap` holds the package power
*caps* alongside the energy counter, so a `g+w` grant there would hand every
member of the group control of the CPU's power limits — through a rule whose
entire purpose is to draw a graph. `cmd/setup_test.go:TestPowercapIsGranted
ReadOnly` checks all four artifacts (both generated, both packaged) and
fails on a `g+w` line mentioning `energy_uj`, because nothing else in the
grant table expresses the distinction.
