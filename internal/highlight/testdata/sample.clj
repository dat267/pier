(ns demo.core
  (:require [clojure.string :as str]))

(def greeting "Hello")

(defn greet [name]
  (let [msg (str greeting ", " name)]
    (println msg)
    {:name name :length (count msg)}))

(greet "world")
