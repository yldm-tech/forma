import type { ReactNode } from "react";
import { NATIVE_INTEGRATIONS } from "../site";
import { Icon, type IconName } from "./icons";
import { useSite } from "./site-context";

// Product illustrations in HTML and CSS rather than screenshots: localized, sharp at any pixel density, and generic. Charts carry no axis numbers or totals, so nothing here reads as usage data. Decorative (aria-hidden): the section text next to each one says what it shows.

const Frame = ({ children, className = "" }: { children: ReactNode; className?: string }) => (
  <div aria-hidden="true" className={`relative select-none ${className}`}>
    <div className="hero-glow pointer-events-none absolute -inset-10" />
    <div className="relative">{children}</div>
  </div>
);

export const SurveyCardArt = () => {
  const { content } = useSite();
  const t = content.common.illustrations;
  return (
    <Frame className="mx-auto w-full max-w-md">
      <div className="card shadow-raised p-6">
        <p className="text-small font-semibold text-teal-700">{t.surveyTitle}</p>
        <p className="mt-2 text-lg leading-7 font-semibold">{t.npsQuestion}</p>
        <div className="mt-4 grid grid-cols-11 gap-1">
          {Array.from({ length: 11 }, (_, i) => (
            <span
              key={i}
              className={`rounded-chip text-small flex h-9 items-center justify-center border ${
                i === 8
                  ? "bg-mint-400 border-teal-600 font-semibold text-teal-900"
                  : "border-line bg-surface-muted text-ink-700"
              }`}>
              {i}
            </span>
          ))}
        </div>
        <div className="text-ink-500 mt-2 flex justify-between text-xs">
          <span>{t.npsLow}</span>
          <span>{t.npsHigh}</span>
        </div>
        <div className="rounded-button border-line text-small text-ink-500 mt-5 border bg-white px-3 py-2.5">
          {t.textPlaceholder}
        </div>
        <div className="mt-5 flex justify-end">
          <span className="btn btn-primary min-h-10 py-2">{t.submit}</span>
        </div>
      </div>
    </Frame>
  );
};

// Literal class names, so Tailwind finds them and no inline style (which the CSP forbids) is needed.
const BAR_WIDTHS = ["w-[86%]", "w-[64%]", "w-[48%]", "w-[30%]"];

export const SummaryChartArt = () => {
  const { content } = useSite();
  const t = content.common.illustrations;
  return (
    <Frame className="mx-auto w-full max-w-md">
      <div className="card shadow-raised p-6">
        <div className="flex items-center gap-3">
          <span className="icon-tile size-9">
            <Icon name="chart" size={18} />
          </span>
          <p className="font-semibold">{t.summaryTitle}</p>
        </div>
        <ul className="mt-6 flex flex-col gap-4">
          {t.summaryOptions.map((option, i) => (
            <li key={option}>
              <p className="text-small text-ink-700">{option}</p>
              <div className="bg-surface-sunken mt-1.5 h-3 rounded-full">
                <div
                  className={`h-3 rounded-full ${BAR_WIDTHS[i]} ${i === 0 ? "bg-teal-600" : "bg-mint-400"}`}
                />
              </div>
            </li>
          ))}
        </ul>
      </div>
    </Frame>
  );
};

const ChainStep = ({ icon, label, value }: { icon: IconName; label: string; value: string }) => (
  <div className="card shadow-raised flex items-center gap-4 p-4">
    <span className="icon-tile size-10">
      <Icon name={icon} size={20} />
    </span>
    <div>
      <p className="text-ink-500 text-xs font-semibold tracking-wide uppercase">{label}</p>
      <p className="font-semibold">{value}</p>
    </div>
  </div>
);

const Connector = () => <div className="bg-mint-500 ml-9 h-6 w-0.5" />;

export const WorkflowChainArt = () => {
  const { content } = useSite();
  const t = content.common.illustrations;
  return (
    <Frame className="mx-auto w-full max-w-sm">
      <ChainStep icon="zap" label={t.workflowTrigger} value={t.workflowTriggerValue} />
      <Connector />
      <ChainStep icon="filter" label={t.workflowFilter} value={t.workflowFilterValue} />
      <Connector />
      <ChainStep icon="mail" label={t.workflowAction} value={t.workflowActionValue} />
    </Frame>
  );
};

const BrowserChrome = ({ children, address }: { children: ReactNode; address: string }) => (
  <div className="card shadow-raised overflow-hidden">
    <div className="border-line bg-surface-muted flex items-center gap-3 border-b px-4 py-2.5">
      <span className="flex gap-1.5">
        <span className="bg-line size-2.5 rounded-full" />
        <span className="bg-line size-2.5 rounded-full" />
        <span className="bg-line size-2.5 rounded-full" />
      </span>
      <span className="rounded-chip text-ink-500 flex-1 bg-white px-3 py-1 text-xs">{address}</span>
    </div>
    <div className="relative h-72 bg-white p-5">{children}</div>
  </div>
);

/** `width` is a Tailwind width class, written out in full at the call site so Tailwind finds it. */
const Skeleton = ({ width, tall = false }: { width: string; tall?: boolean }) => (
  <div className={`bg-surface-sunken rounded-full ${tall ? "h-4" : "h-2.5"} ${width}`} />
);

// Row geometry of DestinationsArt: the rows are h-12 with gap-3, and the connector SVG (h-[288px], five rows) draws to their centres.
const DESTINATION_ROW = 48;
const DESTINATION_GAP = 12;

/** A response fanning out to every place the integrations page names: the native integrations and a webhook. */
export const DestinationsArt = () => {
  const { content } = useSite();
  const t = content.common.illustrations;
  const destinations = [...NATIVE_INTEGRATIONS, "Webhook"];
  const height = destinations.length * DESTINATION_ROW + (destinations.length - 1) * DESTINATION_GAP;
  return (
    <Frame className="mx-auto w-full max-w-lg">
      <div className="grid grid-cols-[minmax(0,1fr)_2.5rem_minmax(0,1fr)] items-center sm:grid-cols-[minmax(0,1fr)_4rem_minmax(0,1fr)]">
        <div className="card shadow-raised p-4">
          <p className="flex items-center gap-2 text-xs font-semibold text-teal-700">
            <span className="bg-mint-500 size-2 rounded-full" />
            {t.newResponse}
          </p>
          <p className="text-small mt-2 font-semibold">{t.npsQuestion}</p>
          <div className="mt-3 flex items-center gap-2">
            <span className="rounded-chip bg-mint-400 text-small flex size-8 flex-none items-center justify-center border border-teal-600 font-semibold text-teal-900">
              8
            </span>
            <Skeleton width="w-full" />
          </div>
        </div>
        <svg
          className="text-mint-500 h-[288px] w-full"
          viewBox={`0 0 40 ${height}`}
          preserveAspectRatio="none"
          fill="none"
          focusable="false">
          {destinations.map((name, i) => {
            const y = i * (DESTINATION_ROW + DESTINATION_GAP) + DESTINATION_ROW / 2;
            return (
              <path
                key={name}
                d={`M0 ${height / 2} C20 ${height / 2} 20 ${y} 40 ${y}`}
                stroke="currentColor"
                strokeWidth={1.5}
                vectorEffect="non-scaling-stroke"
              />
            );
          })}
        </svg>
        <ul className="flex flex-col gap-3">
          {destinations.map((name) => (
            <li key={name} className="card shadow-raised flex h-12 items-center gap-3 px-3">
              <span className="bg-mint-100 text-small inline-flex size-8 flex-none items-center justify-center rounded-[22%] font-bold text-teal-900">
                {name === "Webhook" ? <Icon name="webhook" size={16} /> : name.charAt(0)}
              </span>
              <span className="text-small leading-5 font-semibold">{name}</span>
            </li>
          ))}
        </ul>
      </div>
    </Frame>
  );
};

export const BrowserPopoverArt = () => {
  const { content } = useSite();
  const t = content.common.illustrations;
  return (
    <Frame className="mx-auto w-full max-w-lg">
      <BrowserChrome address="your-site.example/pricing">
        <div className="flex flex-col gap-3">
          <Skeleton width="w-[45%]" tall />
          <Skeleton width="w-[80%]" />
          <Skeleton width="w-[72%]" />
          <div className="mt-3 grid grid-cols-3 gap-3">
            <div className="rounded-button bg-surface-sunken h-20" />
            <div className="rounded-button bg-surface-sunken h-20" />
            <div className="rounded-button bg-surface-sunken h-20" />
          </div>
        </div>
        <div className="card shadow-raised absolute right-4 bottom-4 w-64 p-4">
          <p className="text-[15px] leading-6 font-semibold">{t.popoverQuestion}</p>
          <div className="text-small mt-3 grid grid-cols-2 gap-2">
            <span className="rounded-chip bg-teal-600 px-3 py-1.5 text-center font-semibold text-white">
              {t.popoverYes}
            </span>
            <span className="rounded-chip border-line border px-3 py-1.5 text-center">{t.popoverNo}</span>
          </div>
        </div>
      </BrowserChrome>
    </Frame>
  );
};

export const InAppArt = () => {
  const { content } = useSite();
  const t = content.common.illustrations;
  return (
    <Frame className="mx-auto w-full max-w-lg">
      <BrowserChrome address="app.your-product.example">
        <div className="flex h-full gap-4">
          <div className="border-line flex w-20 flex-col gap-3 border-r pr-4">
            <Skeleton width="w-full" tall />
            <Skeleton width="w-[80%]" />
            <Skeleton width="w-[90%]" />
            <Skeleton width="w-[70%]" />
          </div>
          <div className="flex flex-1 flex-col gap-3">
            <div className="flex flex-wrap gap-2 text-xs">
              <span className="rounded-chip bg-mint-100 px-2 py-1 text-teal-900">
                {t.segmentLabel}: {t.segmentValue}
              </span>
              <span className="rounded-chip bg-surface-sunken text-ink-700 px-2 py-1">
                {t.eventLabel}: {t.eventValue}
              </span>
            </div>
            <Skeleton width="w-[60%]" tall />
            <Skeleton width="w-[85%]" />
            <Skeleton width="w-[75%]" />
          </div>
        </div>
        <div className="card shadow-raised absolute right-4 bottom-4 w-64 p-4">
          <p className="text-[15px] leading-6 font-semibold">{t.ratingQuestion}</p>
          <div className="mt-3 flex gap-1.5">
            {Array.from({ length: 5 }, (_, i) => (
              <span
                key={i}
                className={`rounded-chip size-8 border ${i < 4 ? "bg-mint-400 border-teal-600" : "border-line bg-surface-muted"}`}
              />
            ))}
          </div>
        </div>
      </BrowserChrome>
    </Frame>
  );
};
