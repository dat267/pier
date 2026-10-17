Require Import Coq.Init.Nat.

Theorem plus_0_l : forall n : nat, 0 + n = n.
Proof.
  intros n. simpl. reflexivity.
Qed.

Definition double (n : nat) : nat := n + n.
