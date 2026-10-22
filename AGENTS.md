## Thresh seams

Before finalizing an approach that touches any of these, run `thresh check` and
read the taps. They are facts about your diff, not opinions, and they never block.

- **jenny-ctor** (`choke-point`): in *.go, only newJenny may construct Jenny. Constructing it elsewhere skips what the choke point enforces.
