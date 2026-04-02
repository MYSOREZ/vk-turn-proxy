package main

import (
	"fmt"
	"math/rand"
)

// Английские имена
var enFirstNames = []string{
	"John", "James", "Robert", "Michael", "David",
	"William", "Mary", "Patricia", "Linda", "Barbara",
	"Elizabeth", "Jennifer", "Maria", "Susan", "Margaret",
	"Dorothy", "Lisa", "Nancy", "Karen", "Betty",
	"Helen", "Sandra", "Donna", "Carol", "Ruth", "Sarah",
	"Amy", "Anna", "Rebecca", "Linda", "Barbara",
	"Emily", "Madison", "Olivia", "Ava", "Isabella",
}

// Английские фамилии
var enLastNames = []string{
	"Smith", "Johnson", "Williams", "Brown", "Jones",
	"Garcia", "Miller", "Davis", "Rodriguez", "Martinez",
	"Hernandez", "Lopez", "Gonzalez", "Wilson", "Anderson",
	"Thomas", "Taylor", "Moore", "Jackson", "Martin",
	"Lee", "Perez", "Thompson", "White", "Harris",
	"Sanchez", "Clark", "Ramirez", "Lewis", "Robinson",
	"Walker", "Perez", "Hall", "Young", "Allen",
}

// Арабские кириллические имена
var arabCyrFirstNames = []string{
	"Мухаммад", "Ахмад", "Али", "Умар", "Хасан",
	"Ибрахим", "Юсуф", "Халид", "Абдулла", "Тарик",
	"Билал", "Карим", "Насер", "Самир", "Фатима",
	"Айша", "Лейла", "Нур", "Сара", "Ясмин",
	"Марьям", "Хана", "Рания", "Зайнаб", "Дина",
}

// Арабские кириллические фамилии
var arabCyrLastNames = []string{
	"Ар-Рашид", "Аль-Хасан", "Аль-Фарси", "Аль-Амин", "Аль-Масри",
	"Ас-Сайед", "Аль-Хатиб", "Ан-Наджар", "Аль-Ахмади", "Аль-Мансур",
	"Аль-Халил", "Аль-Бакр", "Ан-Насер", "Аль-Касим", "Аш-Шариф",
	"Аз-Захрани", "Аль-Утайби", "Аль-Гамди", "Аль-Харби", "Аш-Шаммари",
	"Хаддад", "Насар", "Халил", "Баракат", "Таббара",
}

// Арабские латинизированные имена
var arabRomFirstNames = []string{
	"Muhammad", "Ahmad", "Ali", "Umar", "Hassan",
	"Ibrahim", "Yusuf", "Haleed", "Abdullah", "Tarik",
	"Bilal", "Karim", "Nasir", "Samir", "Fatima",
	"Aisha", "Layla", "Nur", "Sara", "Yasmine",
	"Maryam", "Hana", "Rania", "Zainab", "Dina",
}

// Арабские латинизированные фамилии
var arabRomLastNames = []string{
	"Ar-Rashid", "Al-Hasan", "Al-Farsi", "Al-Amin", "Al-Masri",
	"As-Saiyed", "Al-Hatib", "An-Najjar", "Al-Ahmadi", "Al-Mansur",
	"Al-Haleel", "Al-Bakr", "An-Nasir", "Al-Kasim", "Ash-Shari",
	"Az-Zahrani", "Al-Utaibi", "Al-Ghamdi", "Al-Harbi", "Ash-Shammar",
	"Hadad", "Nasir", "Haleel", "Barakat", "Tabbara",
}

// firstNames содержит русские имена.
var rusFirstNames = []string{
	"Александр", "Дмитрий", "Максим", "Сергей", "Андрей", "Алексей", "Артём", "Илья",
	"Кирилл", "Михаил", "Никита", "Матвей", "Роман", "Егор", "Арсений", "Иван",
	"Денис", "Даниил", "Тимофей", "Владислав", "Игорь", "Павел", "Руслан", "Марк",
	"Анна", "Мария", "Елена", "Дарья", "Анастасия", "Екатерина", "Виктория", "Ольга",
	"Наталья", "Юлия", "Татьяна", "Светлана", "Ирина", "Ксения", "Алина", "Елизавета",
}

// lastNames содержит русские фамилии.
var rusLastNames = []string{
	"Иванов", "Смирнов", "Кузнецов", "Попов", "Васильев", "Петров", "Соколов", "Михайлов",
	"Новиков", "Федоров", "Морозов", "Волков", "Алексеев", "Лебедев", "Семенов", "Егоров",
	"Павлов", "Козлов", "Степанов", "Николаев", "Орлов", "Андреев", "Макаров", "Никитин",
	"Захаров", "Зайцев", "Соловьев", "Борисов", "Яковлев", "Григорьев", "Романов", "Воробьев",
}

// nameSource — это одна пара списков имений/фамилий (например русский, английский, арабский кириллический).
type nameSource struct {
	first []string
	last  []string
	isRus bool // Суффикс русской женской фамилии, когда первое имя заканчивается на а/я
}

// Включите буквы: a=arabic, r=russian, e=english.
// Предназначено для использования, если у вас есть фейк аккаунт в вк на определенном регионе и вы хотите скрыть регион.
// По умолчанию: только русский язык
func wrapped_generateName(include string) string {
	var arab, rus, eng bool
	for _, char := range include {
		switch char {
		case 'a':
			arab = true
		case 'r':
			rus = true
		case 'e':
			eng = true
		}
	}

	var pool []nameSource
	if arab {
		if rand.Float32() < 0.5 {
			pool = append(pool, nameSource{arabCyrFirstNames, arabCyrLastNames, false})
		} else {
			pool = append(pool, nameSource{arabRomFirstNames, arabRomLastNames, false})
		}
	}
	if rus {
		pool = append(pool, nameSource{rusFirstNames, rusLastNames, true})
	}
	if eng {
		pool = append(pool, nameSource{enFirstNames, enLastNames, false})
	}

	if len(pool) == 0 {
		return "Xx_NoName_Xx"
	}

	src := pool[rand.Intn(len(pool))]

	if rand.Float32() < 0.3 {
		return src.first[rand.Intn(len(src.first))]
	}

	fn := src.first[rand.Intn(len(src.first))]
	ln := src.last[rand.Intn(len(src.last))]

	if src.isRus {
		lastChar := fn[len(fn)-2:] // 2 байта, так как это кириллица
		if (lastChar == "а" || lastChar == "я") && fn != "Илья" {
			return fmt.Sprintf("%s %sа", fn, ln)
		}
	}
	return fmt.Sprintf("%s %s", fn, ln)
}

func generateName(include string) string {
	if include != "" {
		return wrapped_generateName(include)
	}
	return wrapped_generateName("r")
	// потому что основная аудитория русская, по умолчанию устанавливаем русское имя
}
