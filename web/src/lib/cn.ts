import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/** Joins class names and lets a later Tailwind utility override an earlier one of the same property, as forma2's cn did. */
export const cn = (...inputs: ClassValue[]): string => twMerge(clsx(inputs));
