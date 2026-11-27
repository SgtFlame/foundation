4. **Tools ↔ Cognition.** The `Agent` node runs the tool loop, and tools appear as nodes.
   - *Seam*: `tools.Loop` is built first and called from chat directly. The `Agent` node wraps it later. *Built in slice 8*: chat's `Send` runs the loop, and the `Agent` node (`llm.agent`) wraps it.
