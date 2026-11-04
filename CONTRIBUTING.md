# Pull requests

Every pull request follows the template in `.github/PULL_REQUEST_TEMPLATE.md`, and the `pr-check` workflow fails until it does.

## Title

Use `type(scope): summary`, 72 characters or fewer, with an imperative summary. The scope is optional and lowercase, for example `site`, `learner-repo` or `w5-w8`.

- `content`: new or changed course material such as lessons, exercises, quizzes, solutions and concepts.
- `feat`: new platform or product behaviour.
- `fix`: a bug fix.
- `ui`: a visual change with no behaviour change.
- `test`: tests only, with no content change.
- `docs`: documentation for the repository.
- `chore`: tooling, CI, configuration and dependencies.

Large additions of course material use `content`, never `test`. Example: `content(w5-w8): publish Weeks 5 to 8 and open the course to read`.

## Branches

Name branches `type/short-name`, for example `content/weeks-5-8` or `fix/quiz-parser`.

## Labels

Every pull request has exactly one type label that matches the title type, and at least one area label.

- Type labels: `type: content`, `type: feat`, `type: fix`, `type: ui`, `type: test`, `type: docs`, `type: chore`.
- Area labels: `area: site` for the Next.js app and `area: learner-repo` for the Go and Expo repository. A change that spans both gets both.
- Optional status labels: `needs: deploy-budget` for work held until a batched merge, `blocked` for work that depends on another pull request, and `tech-debt` for cleanup of code that works but is untidy, redundant or harder to change than it should be. Use `tech-debt` together with a type label, usually `type: chore` or `type: fix`.

## Body

- **Summary:** what changed and why, at least 80 characters.
- **Verified:** what you ran or checked and the result.
- **Not verified:** what you did not run. Write the gap plainly instead of leaving it out.
- **Notes:** merge and deploy notes, dependencies on other pull requests and follow-ups. Write `None` if there are none.

The check strips HTML comments, so the placeholder text in the template does not count toward any section.

## Suggested body

```
## Summary

Publishes every Week 5 to 8 item with pages, quizzes, concepts and reference solutions, and opens the course to read without an account.

## Verified

tsc is clean, validateCourse reports 0 errors and all 168 published pages return 200 on localhost.

## Not verified

Dark theme of the sign-up panel and phone layouts.

## Notes

Merging this costs one Netlify deploy. The tests for these exercises are in a separate pull request in the learner repository.
```

## Merging

Use merge commits. Do not squash or rewrite history once review has started; add commits on top. Merges to `main` in the site repository trigger a Netlify deploy, so batch related work into one pull request and label it `needs: deploy-budget` until it is ready.
