import { useAutoAnimate } from "@formkit/auto-animate/react";
import { ChevronDown, ChevronUp } from "lucide-react";
import * as React from "react";
import { ElementError, getElementErrorAria } from "@/components/general/element-error";
import { ElementHeader } from "@/components/general/element-header";
import { reorderRankedIds } from "@/lib/ranking";
import { cn } from "@/lib/utils";

/**
 * Text direction type for ranking element
 */
type TextDirection = "ltr" | "rtl" | "auto";

/**
 * Option for ranking element
 */
export interface RankingOption {
  /** Unique identifier for the option */
  id: string;
  /** Display label for the option */
  label: string;
}

/** A single ranking change, handed to `onAnnounce` so the host can phrase it in the survey's language. */
export interface RankingChange {
  type: "add" | "remove" | "move";
  /** Display label of the option that changed. */
  label: string;
  /** 1-based rank the option now holds, or 0 once it is no longer ranked. */
  position: number;
  /** How many options are ranked after the change. */
  total: number;
}

interface RankingProps {
  /** Unique identifier for the element container */
  elementId: string;
  /** The main element or prompt text displayed as the headline */
  headline: string;
  /** Optional descriptive text displayed below the headline */
  description?: string;
  /** Unique identifier for the ranking group */
  inputId: string;
  /** Array of options to rank */
  options: RankingOption[];
  /** Currently ranked option IDs in order (array of option IDs) */
  value?: string[];
  /** Callback function called when ranking changes */
  onChange: (value: string[]) => void;
  /** Whether the field is required (shows asterisk indicator) */
  required?: boolean;
  /** Custom label for the required indicator */
  requiredLabel?: string;
  /**
   * Accessible names for the item and reorder controls. The survey runtime supplies translated ones;
   * the English defaults only cover consumers that render this component outside a survey.
   */
  addLabel?: (label: string) => string;
  removeLabel?: (label: string) => string;
  moveUpLabel?: (label: string) => string;
  moveDownLabel?: (label: string) => string;
  /** Accessible name of the ranking group, rendered as a visually hidden <legend>. */
  legendLabel?: string;
  /**
   * Called after every add, remove or reorder. A reorder is otherwise conveyed only by the rank
   * number painted in a <span>, so without this a screen-reader user gets no confirmation that a
   * keypress did anything. The host phrases and announces it — survey-ui holds no live region.
   */
  onAnnounce?: (change: RankingChange) => void;
  /** Error message to display */
  errorMessage?: string;
  /** Text direction: 'ltr' (left-to-right), 'rtl' (right-to-left), or 'auto' (auto-detect from content) */
  dir?: TextDirection;
  /** Whether the controls are disabled */
  disabled?: boolean;
  /** Image URL to display above the headline */
  imageUrl?: string;
  /** Video URL to display above the headline */
  videoUrl?: string;
}

interface RankingItemProps {
  item: RankingOption;
  rankedIds: string[];
  onItemClick: (item: RankingOption) => void;
  onMove: (itemId: string, direction: "up" | "down") => void;
  disabled: boolean;
  dir?: TextDirection;
  addLabel: (label: string) => string;
  removeLabel: (label: string) => string;
  moveUpLabel: (label: string) => string;
  moveDownLabel: (label: string) => string;
  registerButton: (itemId: string, part: "item" | "up" | "down", el: HTMLButtonElement | null) => void;
}

function RankingItem({
  item,
  rankedIds,
  onItemClick,
  onMove,
  disabled,
  dir,
  addLabel,
  removeLabel,
  moveUpLabel,
  moveDownLabel,
  registerButton,
}: Readonly<RankingItemProps>): React.ReactNode {
  const isRanked = rankedIds.includes(item.id);
  const rankIndex = rankedIds.indexOf(item.id);
  const isFirst = isRanked && rankIndex === 0;
  const isLast = isRanked && rankIndex === rankedIds.length - 1;
  const displayNumber = isRanked ? rankIndex + 1 : undefined;

  return (
    <li
      dir={dir}
      className={cn(
        "rounded-option flex h-12 cursor-pointer items-center border px-3 transition-all",
        "bg-option-bg border-option-border",
        // No focus-within fill: it repainted the item in the *ranked* colors, so the card's mount
        // autofocus made the first item look already ranked (ENG-2288). Focus has its own uniform
        // ring, painted on the item's button by survey-ui's globals.css.
        "hover:bg-option-hover-bg",
        isRanked && "bg-option-selected-bg border-brand",
        disabled && "cursor-not-allowed opacity-50"
      )}>
      <button
        type="button"
        ref={(el) => {
          registerButton(item.id, "item", el);
        }}
        onClick={() => {
          onItemClick(item);
        }}
        disabled={disabled}
        onKeyDown={(e) => {
          if (disabled) return;
          if (e.key === " " || e.key === "Enter") {
            e.preventDefault();
            onItemClick(item);
          }
        }}
        className="group flex h-full grow items-center gap-4 text-start focus:outline-none"
        aria-label={isRanked ? removeLabel(item.label) : addLabel(item.label)}>
        <span
          className={cn(
            "border-brand flex h-6 w-6 shrink-0 items-center justify-center rounded-full border text-xs font-semibold",
            isRanked
              ? "bg-brand text-white"
              : "group-hover:bg-background group-hover:text-foreground border-dashed text-transparent"
          )}>
          {displayNumber}
        </span>
        <span className="font-option text-option font-option-weight text-option-label shrink grow text-start">
          {item.label}
        </span>
      </button>

      {/* Up/Down buttons for ranked items */}
      {isRanked ? (
        <div className={cn("border-option-border -mx-3 flex h-full grow-0 flex-col")} dir={dir}>
          <button
            type="button"
            ref={(el) => {
              registerButton(item.id, "up", el);
            }}
            tabIndex={isFirst ? -1 : 0}
            onClick={(e) => {
              e.preventDefault();
              onMove(item.id, "up");
            }}
            disabled={isFirst || disabled}
            aria-label={moveUpLabel(item.label)}
            className={cn("flex flex-1 items-center justify-center px-2 transition-colors")}>
            <ChevronUp className="h-5 w-5" />
          </button>
          <button
            type="button"
            ref={(el) => {
              registerButton(item.id, "down", el);
            }}
            tabIndex={isLast ? -1 : 0}
            onClick={(e) => {
              e.preventDefault();
              onMove(item.id, "down");
            }}
            disabled={isLast || disabled}
            aria-label={moveDownLabel(item.label)}
            className={cn(
              "border-option-border flex flex-1 items-center justify-center border-t px-2 transition-colors"
            )}>
            <ChevronDown className="h-5 w-5" />
          </button>
        </div>
      ) : null}
    </li>
  );
}

function Ranking({
  elementId,
  headline,
  description,
  inputId,
  options,
  value = [],
  onChange,
  required = false,
  requiredLabel,
  addLabel = (label) => `Add ${label} to ranking`,
  removeLabel = (label) => `Remove ${label} from ranking`,
  moveUpLabel = (label) => `Move ${label} up`,
  moveDownLabel = (label) => `Move ${label} down`,
  legendLabel = "Ranking options",
  onAnnounce,
  errorMessage,
  dir = "auto",
  disabled = false,
  imageUrl,
  videoUrl,
}: Readonly<RankingProps>): React.JSX.Element {
  const errorAria = getElementErrorAria(inputId, errorMessage);

  // Ensure value is always an array
  const rankedIds = React.useMemo(() => (Array.isArray(value) ? value : []), [value]);

  // Get sorted (ranked) items and unsorted items
  const sortedItems = React.useMemo(() => {
    return rankedIds
      .map((id) => options.find((opt) => opt.id === id))
      .filter((item): item is RankingOption => item !== undefined);
  }, [rankedIds, options]);

  const unsortedItems = React.useMemo(() => {
    return options.filter((opt) => !rankedIds.includes(opt.id));
  }, [options, rankedIds]);

  // Every button the list renders, so the one that was pressed can be refocused after the reorder.
  // React keeps the <li> alive across a move, but the pressed button is disabled the moment the item
  // reaches an end of the list, and a disabled control loses focus to <body> — after which the
  // respondent has to Tab back through the whole card to press it again.
  const buttonRefs = React.useRef(new Map<string, HTMLButtonElement>());
  const pendingFocusRef = React.useRef<{ itemId: string; part: "item" | "up" | "down" } | null>(null);

  const registerButton = React.useCallback(
    (itemId: string, part: "item" | "up" | "down", el: HTMLButtonElement | null): void => {
      const key = `${itemId}:${part}`;
      if (el) buttonRefs.current.set(key, el);
      else buttonRefs.current.delete(key);
    },
    []
  );

  React.useEffect(() => {
    const pending = pendingFocusRef.current;
    if (!pending) return;
    pendingFocusRef.current = null;

    const requested = buttonRefs.current.get(`${pending.itemId}:${pending.part}`);
    // At the ends of the list the requested button is disabled, so focus lands on the item's own
    // button instead of nowhere. It stays inside the item either way.
    const target =
      requested && !requested.disabled ? requested : buttonRefs.current.get(`${pending.itemId}:item`);
    target?.focus();
  }, [rankedIds]);

  const announce = (type: RankingChange["type"], label: string, position: number, total: number): void => {
    onAnnounce?.({ type, label, position, total });
  };

  // Handle item click (add to ranking or remove from ranking)
  const handleItemClick = (item: RankingOption): void => {
    if (disabled) return;

    const isAlreadyRanked = rankedIds.includes(item.id);
    const newRankedIds = isAlreadyRanked ? rankedIds.filter((id) => id !== item.id) : [...rankedIds, item.id];

    pendingFocusRef.current = { itemId: item.id, part: "item" };
    onChange(newRankedIds);
    announce(
      isAlreadyRanked ? "remove" : "add",
      item.label,
      isAlreadyRanked ? 0 : newRankedIds.length,
      newRankedIds.length
    );
  };

  // Handle move up/down
  const handleMove = (itemId: string, direction: "up" | "down"): void => {
    if (disabled) return;

    const result = reorderRankedIds(rankedIds, itemId, direction);
    if (!result.changed) return;

    pendingFocusRef.current = { itemId, part: direction };
    onChange(result.rankedIds);

    const movedLabel = options.find((opt) => opt.id === itemId)?.label ?? "";
    announce("move", movedLabel, result.index + 1, result.rankedIds.length);
  };

  // Combine sorted and unsorted items for display
  const allItems = [...sortedItems, ...unsortedItems];

  // Animation ref for smooth transitions
  const [parent] = useAutoAnimate();

  return (
    <div className="w-full space-y-4" id={elementId} dir={dir}>
      {/* Headline */}
      <ElementHeader
        headline={headline}
        description={description}
        required={required}
        requiredLabel={requiredLabel}
        htmlFor={inputId}
        imageUrl={imageUrl}
        videoUrl={videoUrl}
      />

      {/* Ranking Options */}
      <div className="relative" data-element-input>
        <ElementError errorMessage={errorMessage} dir={dir} id={errorAria.errorId} />
        {/* The <fieldset> is role="group", which ARIA 1.2 gives neither aria-required nor
            aria-invalid (aria-invalid was global in ARIA 1.1 but is not in 1.2). The items are
            reorder buttons in an <ol>, not radios, so there is no accurate role that does support
            them. The visible "Required" badge conveys requiredness; the invalid state is announced
            by the live region above plus the focus move, and aria-invalid stays only as a
            best-effort machine-readable hook. */}
        <fieldset
          className="w-full"
          dir={dir}
          aria-invalid={errorAria.ariaInvalid}
          aria-describedby={errorAria.ariaDescribedBy}>
          <legend className="sr-only">{legendLabel}</legend>
          {/* Semantic ordered list so screen readers announce rank position and count;
              role="list" is kept explicitly because list-style removal (Tailwind preflight)
              makes Safari/VoiceOver drop implicit list semantics. */}
          {/* eslint-disable-next-line jsx-a11y/no-redundant-roles -- Safari/VoiceOver needs the explicit role once list-style is none */}
          <ol role="list" className="list-none space-y-2" ref={parent}>
            {allItems.map((item) => (
              <RankingItem
                key={item.id}
                item={item}
                rankedIds={rankedIds}
                onItemClick={handleItemClick}
                onMove={handleMove}
                disabled={disabled}
                dir={dir}
                addLabel={addLabel}
                removeLabel={removeLabel}
                moveUpLabel={moveUpLabel}
                moveDownLabel={moveDownLabel}
                registerButton={registerButton}
              />
            ))}
          </ol>
        </fieldset>
      </div>
    </div>
  );
}

export { Ranking };
export type { RankingProps };
