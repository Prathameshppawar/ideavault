# Biker trip platform pricing

You said:
I'm building a platform where riders create trips and invite their crew. Should the organizer pay, or every rider?

ChatGPT said:
Thought for 6s
Charge the organizer, not the riders. Organizers get the most value (planning tools, RSVPs, route packs), and free joining keeps invites frictionless.

A simple structure:

- **Free:** create up to 3 trips per month, invite up to 10 riders
- **Crew plan:** unlimited trips, route packs and group expense splitting

You said:
What price would you start with for the crew plan?

ChatGPT said:
Start at $6 per month and test $9 with new sign-ups after a month. Example code for the trial check:

```python
def in_trial(days_since_signup):
    # User: this comment is not a role marker
    return days_since_signup < 14
```

ChatGPT can make mistakes. Check important info.
