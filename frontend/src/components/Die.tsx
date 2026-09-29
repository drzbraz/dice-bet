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

export function Die({ value, rolling }: { value: number | null; rolling?: boolean }) {
  const pips = (value && PIPS[value]) || PIPS[1]!;

  return (
    <div
      className={`grid size-32 grid-cols-3 grid-rows-3 place-items-center rounded-3xl bg-card p-4 ring-1 ring-border ${
        rolling ? "animate-tumble" : "animate-pop-in"
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
  );
}
