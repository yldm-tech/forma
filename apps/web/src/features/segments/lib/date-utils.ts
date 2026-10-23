/**
 * Checks if two dates are on the same day in UTC (ignoring time)
 * @param date1 - The first date
 * @param date2 - The second date
 * @returns True if the dates are on the same UTC day
 */
export const isSameDay = (date1: Date, date2: Date): boolean => {
  return (
    date1.getUTCFullYear() === date2.getUTCFullYear() &&
    date1.getUTCMonth() === date2.getUTCMonth() &&
    date1.getUTCDate() === date2.getUTCDate()
  );
};

/**
 * Converts a date-only string (YYYY-MM-DD) to an ISO string at midnight UTC.
 * This avoids timezone issues where local timezone conversion can shift the day.
 * @param dateString - The date string in YYYY-MM-DD format
 * @returns An ISO string at midnight UTC, or empty string if input is empty
 */
export const toUTCDateString = (dateString: string): string => {
  if (!dateString) return "";
  const [year = Number.NaN, month = Number.NaN, day = Number.NaN] = dateString.split("-").map(Number);
  return new Date(Date.UTC(year, month - 1, day)).toISOString();
};
