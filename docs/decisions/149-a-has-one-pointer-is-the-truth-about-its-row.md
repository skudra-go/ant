---
id: 149
title: "A has-one pointer is the truth about its row"
area: tracking
status: active
tags: [HasOne, SaveChanges, orphan, unlink, relation-reconcile, unique]
issues: [292]
related: [30, 32, 93]
changed_by: []
code: [unit.go]
tests: [tests/has_one_replace_test.go]
---

# 149. A has-one pointer is the truth about its row

**In short:** Replacing or clearing a loaded has-one pointer deletes the row it held, or clears that row's foreign key when the key is nullable, as taking a child out of a slice does. The old row's statement runs before every other write, so a unique key on the foreign key accepts the replacement. A row that moved to another parent is left to that parent's link. A has-one that picks one row of many (`Latest`, `Oldest`) or goes through another table is left alone.

## Decision

Decision 30 made a loaded slice the truth about its relation, and decision 32 made an emptied pointer to a parent unlink. A has-one pointer got neither: `u.Profile = &Profile{...}` inserted a second profile and kept the first, which the next load returned, and `u.Profile = nil` left the row (#292). With a unique foreign key, the usual shape of a one-to-one relation, the replacement failed with a unique violation instead.

`SaveChanges` now treats a has-one pointer as a slice of at most one. The tracker records the child's key when the relation loads or a save writes it, as before. A save whose pointer no longer holds that row orphans it, through the same path as a removed slice element: a required key deletes the row, and a nullable one is set to NULL, with the version and `UpdatedAt` moved on. An edit made to the old child before it was let go is saved with the unlink.

The orphan's statement runs first, before inserts and updates, and its deletion before them too, where the other removals run last. Otherwise the new row takes the foreign key while the old one still holds it, and a unique index refuses the insert. A row the orphaned one owns is the database's to handle on delete, as for any deleted row. A soft delete keeps the row, and its key with it: with a non-nullable key, a unique index on it refuses the new row unless it leaves deleted rows out, as a partial index `WHERE deleted_at IS NULL` does. That is the rule for every unique column of a soft-deleted model, and clearing the key instead would change what a soft delete keeps.

A row another tracked entity's relation holds now has moved, as with slices: `other.Profile = u.Profile; u.Profile = nil` updates the foreign key and deletes nothing. As for slices, a parent counts only if the tracker knows it, loaded or added.

A has-one with `Latest` or `Oldest` picks one row of a has-many, and clearing it says nothing about the rows: they are left alone. A has-one through another table doesn't own its row either. An entity the application removed or detached is not orphaned again.

Rejected: documenting a has-one as read-only for changes. The changing-relations page already says to "change the slice or pointer", and a pointer that silently keeps its old row is the kind of quiet loss the tracker exists to prevent.
