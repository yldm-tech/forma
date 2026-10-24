import type { HTMLAttributes, TdHTMLAttributes, ThHTMLAttributes } from "react";
import { cn } from "../../lib/cn";

export const Table = ({ className, ...props }: HTMLAttributes<HTMLTableElement>) => (
  <div className="relative overflow-auto">
    <table className={cn("w-full caption-bottom text-sm", className)} {...props} />
  </div>
);

export const TableHeader = ({ className, ...props }: HTMLAttributes<HTMLTableSectionElement>) => (
  <thead className={cn("text-slate-800 [&_tr]:border-b", className)} {...props} />
);

export const TableBody = ({ className, ...props }: HTMLAttributes<HTMLTableSectionElement>) => (
  <tbody className={cn("[&_tr:last-child]:border-0", className)} {...props} />
);

export const TableRow = ({ className, ...props }: HTMLAttributes<HTMLTableRowElement>) => (
  <tr className={cn("border-b border-slate-200 bg-white transition-colors", className)} {...props} />
);

export const TableHead = ({ className, ...props }: ThHTMLAttributes<HTMLTableCellElement>) => (
  <th className={cn("h-12 px-4 text-left align-middle font-medium", className)} {...props} />
);

export const TableCell = ({ className, ...props }: TdHTMLAttributes<HTMLTableCellElement>) => (
  <td className={cn("p-4 align-middle", className)} {...props} />
);
