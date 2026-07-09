-- Mark-as-read (finished state). epub.js progress is a fraction through the whole
-- spine including back matter, so a finished story tops out well below 1.0 and
-- the ">= 0.99 = finished" heuristic rarely fires. An explicit finished_at lets a
-- reader mark a book done regardless of scroll position. NULL = not finished.
ALTER TABLE books ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ;
