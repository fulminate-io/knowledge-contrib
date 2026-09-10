// completeness.ts — the COMPLETENESS ASSERTION a walk makes about itself, as a
// value with no usable zero.
//
// WHAT THE ASSERTION DECIDES DOWNSTREAM. It rides through the envelope's
// walk_complete field to the server's deletion guard: a collect asserting a
// complete walk lets the server treat the rows this collect did not carry as
// gone, and a collect asserting an incomplete one disables that phase exactly as
// an incomplete code walk does. Asserting completeness for a walk that gave up
// half way is therefore not a cosmetic error.
//
// WHY IT IS A CLASS AND NOT A boolean. A boolean's default reads as a deliberate
// "incomplete", so a collector author who forgot to think about completeness and
// one who decided the walk was partial produce the same value and are
// indistinguishable to a reviewer. The class below cannot be constructed outside
// this module — its constructor is private and its state lives in ECMAScript
// private fields, which no object literal and no cast can forge — and the walk
// must RETURN one, so an author who omits the decision gets a type error rather
// than a silently incomplete walk.
//
// THE RESIDUAL IS STATED RATHER THAN HIDDEN, and in TypeScript it is wider than
// in the Go original: a collector can cast `undefined`, `null` or a plain object
// through `as unknown as Completeness` into a Result. Every such value is REFUSED
// LOUDLY when the envelope is encoded, naming the collector, which is this
// repository's bad-input invariant applied to its own API. isCompleteness below
// is what makes the refusal decidable at run time.

/**
 * A walk's assertion about whether it enumerated the whole source. Build it with
 * {@link complete} or {@link incomplete}; there is no other way to obtain one,
 * and a value that is not one is refused when a result carrying it is encoded.
 */
export class Completeness {
  readonly #complete: boolean;
  readonly #reason: string;

  private constructor(isComplete: boolean, reason: string) {
    this.#complete = isComplete;
    this.#reason = reason;
  }

  /** @internal — the only construction point, used by {@link complete} and {@link incomplete}. */
  static build(isComplete: boolean, reason: string): Completeness {
    return new Completeness(isComplete, reason);
  }

  /**
   * @internal — the brand check {@link isCompleteness} delegates to. It lives
   * inside the class body because that is the only place the private name is in
   * scope, which is exactly what makes it unforgeable from outside.
   */
  static brandedInstance(value: unknown): value is Completeness {
    return typeof value === "object" && value !== null && #complete in value;
  }

  /** The assertion this value carries. */
  isComplete(): boolean {
    return this.#complete;
  }

  /**
   * The reason an incomplete walk carries, empty on a complete one. A collector
   * logs it; the wire does not — the contract carries a boolean.
   */
  reason(): string {
    return this.#reason;
  }
}

/** Asserts that this walk enumerated the whole source. */
export function complete(): Completeness {
  return Completeness.build(true, "");
}

/**
 * Asserts that this walk did NOT enumerate the whole source, and carries the
 * reason it did not.
 *
 * The reason is not sent on the wire, but it is required all the same: it is what
 * a collector author writes into their own logs and what a reviewer reads to
 * judge whether the arm is reachable at all. An empty reason is refused when the
 * result is encoded, on the same terms as a value that was never constructed.
 */
export function incomplete(reason: string): Completeness {
  return Completeness.build(false, reason);
}

/**
 * Reports whether a value is a Completeness this module built. It is the run-time
 * half of the type's guarantee: TypeScript's structural typing and its casts make
 * the compile-time half evadable, so the encoder decides by this.
 */
export function isCompleteness(value: unknown): value is Completeness {
  // The ECMAScript private-field brand check: true only for an object this
  // module's constructor ran on, so a duck carrying the same two methods, and a
  // bare Object.create of the prototype, are both false.
  return Completeness.brandedInstance(value);
}
