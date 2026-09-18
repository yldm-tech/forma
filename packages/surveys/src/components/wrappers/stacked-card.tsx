import { MutableRef } from "preact/hooks";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { JSX } from "preact/jsx-runtime";
import React from "react";
import { type TPlacement } from "@forma/types/common";
import { TJsWorkspaceStateSurvey } from "@forma/types/js";
import { TCardArrangementOptions } from "@forma/types/styling";

interface StackedCardProps {
  cardRefs: MutableRef<(HTMLDivElement | null)[]>;
  dynamicQuestionIndex: number;
  offset: number;
  fullSizeCards: boolean;
  borderStyles: React.CSSProperties;
  getCardContent: (questionIdxTemp: number, offset: number) => JSX.Element | undefined;
  cardHeight: string;
  survey: TJsWorkspaceStateSurvey;
  cardWidth: number;
  hovered: boolean;
  cardArrangement: TCardArrangementOptions;
  placement: TPlacement;
}

export const StackedCard = ({
  cardRefs,
  dynamicQuestionIndex,
  offset,
  fullSizeCards,
  borderStyles,
  getCardContent,
  cardHeight,
  survey,
  cardWidth,
  hovered,
  cardArrangement,
  placement,
}: StackedCardProps) => {
  const isHidden = offset < 0;
  const [delayedOffset, setDelayedOffset] = useState<number>(offset);
  // 1 on the first card so it is legible as soon as it paints; the fade below still applies to every
  // card-to-card transition after that.
  const [contentOpacity, setContentOpacity] = useState<number>(offset === 0 ? 1 : 0);
  const hasMountedRef = useRef(offset === 0);
  const currentCardHeight = offset === 0 ? "auto" : offset < 0 ? "initial" : cardHeight;

  const getTopBottomStyles = () => {
    if (survey.type !== "link")
      if (placement === "bottomLeft" || placement === "bottomRight") {
        return {
          bottom: 0,
        };
      } else if (placement === "topLeft" || placement === "topRight") {
        return {
          top: 0,
        };
      }
  };

  const getDummyCardContent = () => {
    return <div style={{ height: cardHeight }} className="w-full p-6"></div>;
  };

  const calculateCardTransform = useMemo(() => {
    let rotationCoefficient = 3;

    if (cardWidth >= 1000) {
      rotationCoefficient = 1.5;
    } else if (cardWidth > 650) {
      rotationCoefficient = 2;
    }

    return (offset: number) => {
      switch (cardArrangement) {
        case "casual":
          return offset < 0
            ? `translateX(33%)`
            : `translateX(0) rotate(-${((hovered ? rotationCoefficient : rotationCoefficient - 0.5) * offset).toString()}deg)`;
        case "straight":
          return offset < 0
            ? `translateY(25%)`
            : `translateY(-${((hovered ? 12 : 10) * offset).toString()}px)`;
        default:
          return `translateX(0)`;
      }
    };
  }, [cardArrangement, hovered, cardWidth]);

  const straightCardArrangementStyles =
    cardArrangement === "straight"
      ? {
          width: `${(100 - 5 * offset >= 100 ? 100 : 100 - 5 * offset).toString()}%`,
          margin: "auto",
        }
      : {};

  useEffect(() => {
    const offsetTimer = setTimeout(() => {
      setDelayedOffset(offset);
    }, 300);

    // The fade belongs to moving between cards. On the very first card there is nothing to move from,
    // and paying for it meant a respondent waited 300 ms of delay plus a 300 ms fade to read a
    // question that had already rendered — measured at 971 ms to become legible with the loading
    // overlay forced off, most of it here.
    if (offset === 0 && !hasMountedRef.current) {
      hasMountedRef.current = true;
      setContentOpacity(1);
      return () => {
        clearTimeout(offsetTimer);
      };
    }

    let opacityTimer: ReturnType<typeof setTimeout> | undefined;
    if (offset === 0) {
      setContentOpacity(0);
      opacityTimer = setTimeout(() => {
        setContentOpacity(1);
      }, 300);
    }

    return () => {
      clearTimeout(offsetTimer);
      if (opacityTimer) clearTimeout(opacityTimer);
    };
  }, [offset]);

  return (
    <div
      ref={(el) => {
        cardRefs.current[dynamicQuestionIndex] = el;
      }}
      id={`questionCard-${dynamicQuestionIndex}`}
      data-testid={`questionCard-${dynamicQuestionIndex}`}
      key={dynamicQuestionIndex}
      style={{
        zIndex: 1000 - dynamicQuestionIndex,
        transform: calculateCardTransform(offset),
        opacity: isHidden ? 0 : (100 - 20 * offset) / 100,
        height: fullSizeCards ? "100%" : currentCardHeight,
        transition: "transform 600ms ease-in-out, opacity 600ms ease-in-out, width 600ms ease-in-out",
        pointerEvents: offset === 0 ? "auto" : "none",
        ...borderStyles,
        ...straightCardArrangementStyles,
        ...getTopBottomStyles(),
      }}
      className="pointer rounded-custom bg-survey-bg absolute inset-x-0 overflow-hidden">
      <div
        style={{
          opacity: contentOpacity,
          transition: "opacity 300ms ease-in-out",
          height: "100%",
        }}>
        {delayedOffset === 0 ? getCardContent(dynamicQuestionIndex, offset) : getDummyCardContent()}
      </div>
    </div>
  );
};
