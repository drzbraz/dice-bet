const PIPS: Record<number, Array<[number, number]>> = {
  1: [[2, 2]],
  2: [
    [1, 1],
    [3, 3],
  ],
  3: [
    [1, 1],
    [2, 2],
    [3, 3],
  ],
  4: [
    [1, 1],
    [1, 3],
    [3, 1],
    [3, 3],
  ],
  5: [
    [1, 1],
    [1, 3],
    [2, 2],
    [3, 1],
    [3, 3],
  ],
  6: [
    [1, 1],
    [1, 3],
    [2, 1],
    [2, 3],
    [3, 1],
    [3, 3],
  ],
};

// Small burst of pieces flung outward from the die's center on a win, each
// with its own end position/rotation/delay via CSS custom properties (see
// the confetti-burst keyframes in styles.css).
const CONFETTI = [
  { x: -58, y: -74, rotate: -35, delay: 0 },
  { x: 46, y: -86, rotate: 24, delay: 0.05 },
  { x: -28, y: -96, rotate: 58, delay: 0.1 },
  { x: 66, y: -46, rotate: -18, delay: 0.03 },
  { x: -70, y: -36, rotate: 42, delay: 0.08 },
  { x: 18, y: -100, rotate: -48, delay: 0.13 },
  { x: -46, y: 60, rotate: 30, delay: 0.06 },
  { x: 52, y: 66, rotate: -40, delay: 0.11 },
];

export function Die({
  value,
  rolling,
  won,
}: {
  value: number | null;
  rolling?: boolean;
  won?: boolean;
}) {
  const pips = (value && PIPS[value]) || PIPS[1]!;

  return (
    <div className="relative">
      {won && (
        <div className="pointer-events-none absolute inset-0" aria-hidden="true">
          {CONFETTI.map((piece, i) => (
            <span
              key={i}
              className={`absolute top-1/2 left-1/2 size-2 rounded-sm animate-confetti ${
                i % 2 === 0 ? "bg-accent" : "bg-success"
              }`}
              style={
                {
                  "--confetti-x": `${piece.x}px`,
                  "--confetti-y": `${piece.y}px`,
                  "--confetti-rotate": `${piece.rotate}deg`,
                  animationDelay: `${piece.delay}s`,
                } as React.CSSProperties
              }
            />
          ))}
        </div>
      )}
      <div
        className={`grid size-32 grid-cols-3 grid-rows-3 place-items-center rounded-3xl bg-card p-4 ring-1 ring-border ${
          rolling ? "animate-tumble" : won ? "animate-win-pop animate-win-glow" : "animate-pop-in"
        }`}
        style={{ boxShadow: "var(--shadow-die)" }}
        aria-label={value ? `Die showing ${value}` : "Die ready to roll"}
      >
        {pips.map(([row, col], i) => (
          <span
            key={i}
            className="size-4 rounded-full bg-gradient-warm"
            style={{ gridRow: row, gridColumn: col }}
          />
        ))}
      </div>
    </div>
  );
}
