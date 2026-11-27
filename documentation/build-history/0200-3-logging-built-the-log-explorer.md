3. **Logging.** *Built.* ★ the Log Explorer.
   - The VictoriaLogs handler, `scripts/victorialogs.sh`, the `logging` section, and `logsservice`; *built*
   - The Log Explorer with the builder, the LogsQL editor, the histogram and live tail; *built*, with quick filters, *Show this trace*, surrounding records, deep links, and *Show in Log Explorer* from DevTools' Log tab. Saved queries wait.
   - Browser log forwarding (`warn` and above) with a shared trace; *built*. Full logging for one user waits for identity (slice 10).
   - What slice 3 decided is recorded in [Logging.md](Logging.md#built-in-slice-3).
