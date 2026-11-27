3. **Chronicle ↔ Cognition.** *Personal* mode is a Cognition graph, but the node library is shaped by Chronicle's steps.
   - *Seam*: Chronicle's pipeline is first written in Go as separate step functions with typed inputs and outputs (window, retrieve, match, reply, update), each shaped like a node.
   - Cognition then wraps each step as a node, and the built-in *Personal* graph replaces the Go sequence. The same tests prove it behaves identically.
